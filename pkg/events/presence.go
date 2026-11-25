package events

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	PresenceOffline   = "offline"
	PresenceOnline    = "online"
	PresenceAway      = "away"
	PresenceInMeeting = "in_meeting"

	PresenceConnTTL    = 45 * time.Second
	PresenceMeetingTTL = 4 * time.Hour
)

type PresenceRecord struct {
	UserID       string `json:"userID"`
	Status       string `json:"status"` // offline | online | away | in_meeting
	MeetingID    string `json:"meetingID,omitempty"`
	UpdatedAtMs  int64  `json:"updatedAtMs"`
}

func presenceConnKey(userID string) string {
	return "om:presence:conn:" + userID
}

func presenceMeetingKey(userID string) string {
	return "om:presence:meeting:" + userID
}

// SynthesizePresence computes effective presence strictly following the priority:
// Offline -> InMeeting (highest online priority) -> Away -> Online.
func SynthesizePresence(userID string, connAlive bool, manualState string, meetingID string, updatedAtMs int64) PresenceRecord {
	rec := PresenceRecord{
		UserID:      userID,
		UpdatedAtMs: updatedAtMs,
	}
	if meetingID != "" {
		rec.Status = PresenceInMeeting
		rec.MeetingID = meetingID
		return rec
	}
	if !connAlive {
		rec.Status = PresenceOffline
		return rec
	}
	if strings.EqualFold(strings.TrimSpace(manualState), PresenceAway) {
		rec.Status = PresenceAway
		return rec
	}
	rec.Status = PresenceOnline
	return rec
}

func (h *Hub) SetConnectionOnline(ctx context.Context, userID, connID, gatewayID, manualState string) (PresenceRecord, error) {
	now := time.Now().UnixMilli()
	if manualState != PresenceAway {
		manualState = PresenceOnline
	}
	if h == nil || h.rdb == nil {
		return SynthesizePresence(userID, true, manualState, "", now), nil
	}
	key := presenceConnKey(userID)
	pipe := h.rdb.Pipeline()
	pipe.HSet(ctx, key, map[string]any{
		"conn_id":      connID,
		"gateway_id":   gatewayID,
		"manual_state": manualState,
		"updated_ms":   now,
	})
	pipe.Expire(ctx, key, PresenceConnTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return PresenceRecord{}, err
	}
	rec, err := h.GetPresence(ctx, userID)
	if err != nil {
		return PresenceRecord{}, err
	}
	_ = h.publishPresence(ctx, rec)
	return rec, nil
}

func (h *Hub) RefreshConnection(ctx context.Context, userID, connID, manualState string) (PresenceRecord, bool, error) {
	now := time.Now().UnixMilli()
	if manualState != PresenceAway {
		manualState = PresenceOnline
	}
	if h == nil || h.rdb == nil {
		return SynthesizePresence(userID, true, manualState, "", now), false, nil
	}
	key := presenceConnKey(userID)
	prev, err := h.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return PresenceRecord{}, false, err
	}
	if prev["conn_id"] != "" && prev["conn_id"] != connID {
		return PresenceRecord{}, false, nil
	}
	changed := prev["manual_state"] != manualState || prev["conn_id"] == ""
	pipe := h.rdb.Pipeline()
	pipe.HSet(ctx, key, map[string]any{
		"conn_id":      connID,
		"manual_state": manualState,
		"updated_ms":   now,
	})
	pipe.Expire(ctx, key, PresenceConnTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return PresenceRecord{}, false, err
	}
	rec, err := h.GetPresence(ctx, userID)
	if err != nil {
		return PresenceRecord{}, false, err
	}
	if changed {
		_ = h.publishPresence(ctx, rec)
	}
	return rec, changed, nil
}

var clearConnScript = goredis.NewScript(`
if redis.call("HGET", KEYS[1], "conn_id") == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
`)

func (h *Hub) ClearConnection(ctx context.Context, userID, connID string) error {
	if h == nil || h.rdb == nil || userID == "" {
		return nil
	}
	deleted, err := clearConnScript.Run(ctx, h.rdb, []string{presenceConnKey(userID)}, connID).Int()
	if err != nil {
		return err
	}
	if deleted > 0 {
		rec, err := h.GetPresence(ctx, userID)
		if err == nil {
			_ = h.publishPresence(ctx, rec)
		}
	}
	return nil
}

func (h *Hub) SetUserInMeeting(ctx context.Context, userID, meetingID string) error {
	if h == nil || h.rdb == nil || userID == "" || meetingID == "" {
		return nil
	}
	if err := h.rdb.Set(ctx, presenceMeetingKey(userID), meetingID, PresenceMeetingTTL).Err(); err != nil {
		return err
	}
	rec, err := h.GetPresence(ctx, userID)
	if err == nil {
		_ = h.publishPresence(ctx, rec)
	}
	return nil
}

var clearMeetingScript = goredis.NewScript(`
local cur = redis.call("GET", KEYS[1])
if not cur then
    return 0
end
if ARGV[1] == "" or cur == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
`)

func (h *Hub) ClearUserInMeeting(ctx context.Context, userID, meetingID string) error {
	if h == nil || h.rdb == nil || userID == "" {
		return nil
	}
	deleted, err := clearMeetingScript.Run(ctx, h.rdb, []string{presenceMeetingKey(userID)}, meetingID).Int()
	if err != nil {
		return err
	}
	if deleted > 0 {
		rec, err := h.GetPresence(ctx, userID)
		if err == nil {
			_ = h.publishPresence(ctx, rec)
		}
	}
	return nil
}

func (h *Hub) GetUserInMeeting(ctx context.Context, userID string) (string, error) {
	if h == nil || h.rdb == nil || userID == "" {
		return "", nil
	}
	val, err := h.rdb.Get(ctx, presenceMeetingKey(userID)).Result()
	if err == goredis.Nil {
		return "", nil
	}
	return val, err
}

func (h *Hub) GetPresence(ctx context.Context, userID string) (PresenceRecord, error) {
	batch, err := h.GetPresenceBatch(ctx, []string{userID})
	if err != nil {
		return PresenceRecord{}, err
	}
	if rec, ok := batch[userID]; ok {
		return rec, nil
	}
	return SynthesizePresence(userID, false, "", "", time.Now().UnixMilli()), nil
}

func (h *Hub) GetPresenceBatch(ctx context.Context, userIDs []string) (map[string]PresenceRecord, error) {
	out := make(map[string]PresenceRecord, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	now := time.Now().UnixMilli()
	if h == nil || h.rdb == nil {
		for _, id := range userIDs {
			out[id] = SynthesizePresence(id, false, "", "", now)
		}
		return out, nil
	}
	pipe := h.rdb.Pipeline()
	connCmds := make(map[string]*goredis.MapStringStringCmd, len(userIDs))
	meetCmds := make(map[string]*goredis.StringCmd, len(userIDs))
	for _, id := range userIDs {
		if id == "" {
			continue
		}
		if _, dup := connCmds[id]; dup {
			continue
		}
		connCmds[id] = pipe.HGetAll(ctx, presenceConnKey(id))
		meetCmds[id] = pipe.Get(ctx, presenceMeetingKey(id))
	}
	_, _ = pipe.Exec(ctx)
	for id, cCmd := range connCmds {
		cMap, _ := cCmd.Result()
		mID, mErr := meetCmds[id].Result()
		if mErr == goredis.Nil {
			mID = ""
		}
		alive := len(cMap) > 0 && cMap["conn_id"] != ""
		out[id] = SynthesizePresence(id, alive, cMap["manual_state"], mID, now)
	}
	return out, nil
}

func (h *Hub) publishPresence(ctx context.Context, rec PresenceRecord) error {
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return h.rdb.Publish(ctx, PresencePubSubChannel(), raw).Err()
}
