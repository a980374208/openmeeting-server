package livekit

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/livekit/protocol/livekit"
	"github.com/openimsdk/openmeeting-server/pkg/rtc"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

const (
	AgentIdentityPrefix       = "lk-agent-"
	defaultAgentTokenTTLSec   = 15 * 60
	participantKindAgentValue = 4
)

type agentParticipantMetadata struct {
	Role           string `json:"role"`
	AgentRole      string `json:"agentRole,omitempty"`
	OwnerUserID    string `json:"ownerUserID,omitempty"`
	SourceLanguage string `json:"sourceLanguage,omitempty"`
	TargetLanguage string `json:"targetLanguage,omitempty"`
}

type liveKitAgentVideoGrant struct {
	RoomJoin             bool   `json:"roomJoin"`
	Room                 string `json:"room"`
	CanPublish           bool   `json:"canPublish"`
	CanSubscribe         bool   `json:"canSubscribe"`
	CanPublishData       bool   `json:"canPublishData"`
	CanUpdateOwnMetadata bool   `json:"canUpdateOwnMetadata"`
	Hidden               bool   `json:"hidden"`
}

type liveKitAgentClaims struct {
	jwt.RegisteredClaims
	Name       string                 `json:"name,omitempty"`
	Metadata   string                 `json:"metadata,omitempty"`
	Kind       string                 `json:"kind,omitempty"`
	Attributes map[string]string      `json:"attributes,omitempty"`
	Video      liveKitAgentVideoGrant `json:"video"`
}

// GetAgentJoinToken signs a minimal-privilege LiveKit AGENT JWT for a client acting
// as the room's transcription/translation agent (data-only: no media publish/subscribe).
func (x *LiveKit) GetAgentJoinToken(ctx context.Context, opts rtc.AgentTokenOptions) (string, string, int64, error) {
	if strings.TrimSpace(opts.RoomID) == "" {
		return "", "", 0, errs.ErrArgs.WrapMsg("roomID is required")
	}
	if strings.TrimSpace(opts.AgentIdentity) == "" {
		return "", "", 0, errs.ErrArgs.WrapMsg("agentIdentity is required")
	}
	ttl := opts.TTLSeconds
	if ttl <= 0 {
		ttl = defaultAgentTokenTTLSec
	}
	now := time.Now()
	expiresAt := now.Add(time.Duration(ttl) * time.Second)

	role := strings.TrimSpace(opts.Role)
	if role == "" {
		role = "translator"
	}
	displayName := sanitizeAgentLabel(opts.DisplayName, 64)
	if displayName == "" {
		displayName = "AI Agent"
	}

	attrs := map[string]string{
		"lk.agent.state":      "initializing",
		"cohavora.agent.role": role,
	}
	if opts.OwnerUserID != "" {
		attrs["cohavora.agent.owner"] = opts.OwnerUserID
	}
	if opts.SourceLanguage != "" {
		attrs["cohavora.agent.source_lang"] = opts.SourceLanguage
	}
	if opts.TargetLanguage != "" {
		attrs["cohavora.agent.target_lang"] = opts.TargetLanguage
	}

	metaBytes, err := json.Marshal(agentParticipantMetadata{
		Role:           "agent",
		AgentRole:      role,
		OwnerUserID:    opts.OwnerUserID,
		SourceLanguage: opts.SourceLanguage,
		TargetLanguage: opts.TargetLanguage,
	})
	if err != nil {
		return "", "", 0, errs.WrapMsg(err, "marshal agent metadata failed")
	}

	claims := liveKitAgentClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    x.conf.ApiKey,
			Subject:   opts.AgentIdentity,
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		Name:       displayName,
		Metadata:   string(metaBytes),
		Kind:       "agent",
		Attributes: attrs,
		Video: liveKitAgentVideoGrant{
			RoomJoin:             true,
			Room:                 opts.RoomID,
			CanPublish:           false,
			CanSubscribe:         false,
			CanPublishData:       true,
			CanUpdateOwnMetadata: true,
			Hidden:               false,
		},
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(x.conf.ApiSecret))
	if err != nil {
		return "", "", 0, errs.WrapMsg(err, "sign livekit agent jwt failed")
	}
	log.ZDebug(ctx, "issued livekit agent token", "roomID", opts.RoomID, "agentIdentity", opts.AgentIdentity, "role", role)
	return signed, x.getLiveURL(), expiresAt.Unix(), nil
}

// ListOnlineAgents inspects live participants in a LiveKit room and returns all
// active top-level AGENT participants (excluding disconnected and publish-on-behalf workers).
func (x *LiveKit) ListOnlineAgents(ctx context.Context, roomID string) ([]rtc.OnlineAgentInfo, error) {
	participants, err := x.ListParticipants(ctx, roomID)
	if err != nil {
		if x.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	agents := make([]rtc.OnlineAgentInfo, 0)
	for _, p := range participants {
		if p == nil || p.GetState() == livekit.ParticipantInfo_DISCONNECTED {
			continue
		}
		attrs := extractParticipantAttributes(p)
		if attrs["lk.publish_on_behalf"] != "" {
			continue
		}
		var meta agentParticipantMetadata
		if raw := strings.TrimSpace(p.GetMetadata()); raw != "" && strings.HasPrefix(raw, "{") {
			_ = json.Unmarshal([]byte(raw), &meta)
		}

		isAgent := extractParticipantKindInt(p) == participantKindAgentValue ||
			strings.HasPrefix(p.GetIdentity(), AgentIdentityPrefix) ||
			strings.EqualFold(meta.Role, "agent")
		if !isAgent {
			continue
		}

		info := rtc.OnlineAgentInfo{
			Identity:       p.GetIdentity(),
			Name:           p.GetName(),
			OwnerUserID:    firstNonEmpty(attrs["cohavora.agent.owner"], meta.OwnerUserID),
			Role:           firstNonEmpty(attrs["cohavora.agent.role"], meta.AgentRole, "agent"),
			State:          firstNonEmpty(attrs["lk.agent.state"], "online"),
			TargetLanguage: firstNonEmpty(attrs["cohavora.agent.target_lang"], meta.TargetLanguage),
		}
		agents = append(agents, info)
	}
	return agents, nil
}

func extractParticipantAttributes(p *livekit.ParticipantInfo) map[string]string {
	type attrGetter interface {
		GetAttributes() map[string]string
	}
	if g, ok := any(p).(attrGetter); ok {
		if attrs := g.GetAttributes(); attrs != nil {
			return attrs
		}
	}
	return map[string]string{}
}

func extractParticipantKindInt(p *livekit.ParticipantInfo) int64 {
	v := reflect.ValueOf(p)
	if !v.IsValid() || v.IsNil() {
		return 0
	}
	if method := v.MethodByName("GetKind"); method.IsValid() && method.Type().NumIn() == 0 && method.Type().NumOut() == 1 {
		out := method.Call(nil)[0]
		switch out.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return out.Int()
		}
	}
	return 0
}

func sanitizeAgentLabel(raw string, maxRunes int) string {
	var b strings.Builder
	count := 0
	for _, r := range strings.TrimSpace(raw) {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
		count++
		if count >= maxRunes {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
