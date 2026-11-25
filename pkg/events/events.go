package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	MaxInboxEvents = 500
	InboxTTL       = 7 * 24 * time.Hour

	TypeHelloOK                = "hello_ok"
	TypePong                   = "pong"
	TypeSessionKicked          = "session.kicked"
	TypeContactRelationUpdated = "contact.relation_updated"
	TypeContactRequestReceived = "contact.request_received"
	TypeContactRequestHandled  = "contact.request_handled"
	TypeOrgTreeUpdated         = "org.tree_updated"
	TypeMeetingInvited         = "meeting.invited"
	TypePresenceChangedBatch   = "presence.changed_batch"
	TypeCallIncoming           = "call.incoming"
	TypeCallAccepted           = "call.accepted"
	TypeCallRejected           = "call.rejected"
	TypeCallCancelled          = "call.cancelled"
	TypeCallTimeout            = "call.timeout"
)

type Envelope struct {
	Type        string          `json:"type"`
	Seq         int64           `json:"seq,omitempty"`
	TimestampMs int64           `json:"timestampMs"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

func UserPubSubChannel(userID string) string {
	return "om:pubsub:user:" + userID
}

func PresencePubSubChannel() string {
	return "om:pubsub:presence"
}

func OrgBroadcastPubSubChannel() string {
	return "om:pubsub:org"
}

func userSeqKey(userID string) string {
	return "om:evt:seq:" + userID
}

func userInboxKey(userID string) string {
	return "om:evt:inbox:" + userID
}

type Hub struct {
	rdb goredis.UniversalClient
}

func NewHub(rdb goredis.UniversalClient) *Hub {
	if rdb == nil {
		return nil
	}
	return &Hub{rdb: rdb}
}

// PublishDurableEvent allocates a per-user monotonic seq, stores the envelope
// in a bounded 500-item ZSet window, and pushes it over Redis Pub/Sub.
func (h *Hub) PublishDurableEvent(ctx context.Context, userID, eventType string, payload any) (int64, error) {
	if h == nil || h.rdb == nil || userID == "" {
		return 0, nil
	}
	seq, err := h.rdb.Incr(ctx, userSeqKey(userID)).Result()
	if err != nil {
		return 0, err
	}
	_ = h.rdb.Expire(ctx, userSeqKey(userID), InboxTTL).Err()

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	env := Envelope{
		Type:        eventType,
		Seq:         seq,
		TimestampMs: time.Now().UnixMilli(),
		Payload:     rawPayload,
	}
	rawEnv, err := json.Marshal(env)
	if err != nil {
		return 0, err
	}

	inbox := userInboxKey(userID)
	pipe := h.rdb.Pipeline()
	pipe.ZAdd(ctx, inbox, goredis.Z{Score: float64(seq), Member: string(rawEnv)})
	pipe.ZRemRangeByRank(ctx, inbox, 0, -MaxInboxEvents-1)
	pipe.Expire(ctx, inbox, InboxTTL)
	pipe.Publish(ctx, UserPubSubChannel(userID), rawEnv)
	_, err = pipe.Exec(ctx)
	return seq, err
}

// PublishEphemeralEvent delivers real-time signals (e.g., call signaling,
// session.kicked) without polluting the durable sync inbox.
func (h *Hub) PublishEphemeralEvent(ctx context.Context, userID, eventType string, payload any) error {
	if h == nil || h.rdb == nil || userID == "" {
		return nil
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	env := Envelope{
		Type:        eventType,
		TimestampMs: time.Now().UnixMilli(),
		Payload:     rawPayload,
	}
	rawEnv, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return h.rdb.Publish(ctx, UserPubSubChannel(userID), rawEnv).Err()
}

type SessionKickedPayload struct {
	Reason    string `json:"reason"`
	KeepToken string `json:"keepToken,omitempty"`
}

func (h *Hub) PublishSessionKicked(ctx context.Context, userID, reason, keepToken string) error {
	return h.PublishEphemeralEvent(ctx, userID, TypeSessionKicked, SessionKickedPayload{
		Reason:    reason,
		KeepToken: keepToken,
	})
}

func (h *Hub) PublishOrgTreeUpdated(ctx context.Context, version int64) error {
	if h == nil || h.rdb == nil {
		return nil
	}
	rawPayload, err := json.Marshal(map[string]any{"treeVersion": version})
	if err != nil {
		return err
	}
	env := Envelope{
		Type:        TypeOrgTreeUpdated,
		TimestampMs: time.Now().UnixMilli(),
		Payload:     rawPayload,
	}
	rawEnv, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return h.rdb.Publish(ctx, OrgBroadcastPubSubChannel(), rawEnv).Err()
}

// ListSince returns missed durable events with seq > sinceSeq.
// If sinceSeq == 0 or falls behind the retained ZSet window, syncResetRequired is true.
func (h *Hub) ListSince(ctx context.Context, userID string, sinceSeq int64) (events []Envelope, latestSeq int64, syncResetRequired bool, err error) {
	if h == nil || h.rdb == nil || userID == "" {
		return nil, 0, sinceSeq <= 0, nil
	}
	seqStr, err := h.rdb.Get(ctx, userSeqKey(userID)).Result()
	if err != nil && err != goredis.Nil {
		return nil, 0, false, err
	}
	if seqStr != "" {
		latestSeq, _ = strconv.ParseInt(seqStr, 10, 64)
	}
	if sinceSeq <= 0 {
		return nil, latestSeq, true, nil
	}
	if latestSeq == 0 || sinceSeq >= latestSeq {
		return []Envelope{}, latestSeq, false, nil
	}
	inbox := userInboxKey(userID)
	oldest, err := h.rdb.ZRangeWithScores(ctx, inbox, 0, 0).Result()
	if err != nil {
		return nil, latestSeq, false, err
	}
	if len(oldest) == 0 || int64(oldest[0].Score) > sinceSeq+1 {
		return nil, latestSeq, true, nil
	}
	members, err := h.rdb.ZRangeByScore(ctx, inbox, &goredis.ZRangeBy{
		Min: fmt.Sprintf("(%d", sinceSeq),
		Max: "+inf",
	}).Result()
	if err != nil {
		return nil, latestSeq, false, err
	}
	events = make([]Envelope, 0, len(members))
	for _, m := range members {
		var env Envelope
		if uErr := json.Unmarshal([]byte(m), &env); uErr == nil {
			events = append(events, env)
		}
	}
	return events, latestSeq, false, nil
}
