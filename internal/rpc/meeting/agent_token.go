package meeting

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openimsdk/openmeeting-server/pkg/agentrpc"
	"github.com/openimsdk/openmeeting-server/pkg/common"
	"github.com/openimsdk/openmeeting-server/pkg/common/constant"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/rtc"
	"github.com/openimsdk/openmeeting-server/pkg/rtc/livekit"
	sysConstant "github.com/openimsdk/protocol/constant"
	pbmeeting "github.com/openimsdk/protocol/openmeeting/meeting"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
)

const (
	agentClaimLockTTL    = 5 * time.Second
	agentPendingClaimTTL = 6 * time.Second
	agentTokenTTLSeconds = 15 * 60
)

type pendingAgentClaim struct {
	Info      agentrpc.ExistingAgentInfo `json:"info"`
	ExpiresAt time.Time                  `json:"expiresAt"`
}

func (s *meetingServer) getRoomAgentLock(meetingID string) *sync.Mutex {
	s.agentLockMu.Lock()
	defer s.agentLockMu.Unlock()
	if s.agentRoomLocks == nil {
		s.agentRoomLocks = make(map[string]*sync.Mutex)
	}
	mu, ok := s.agentRoomLocks[meetingID]
	if !ok {
		mu = &sync.Mutex{}
		s.agentRoomLocks[meetingID] = mu
	}
	return mu
}

func (s *meetingServer) acquireDistributedAgentLock(ctx context.Context, meetingID string) (func(), error) {
	if s.rdb == nil {
		return func() {}, nil
	}
	key := "meeting:agent:lock:" + meetingID
	token := randomHex(8)
	ok, err := s.rdb.SetNX(ctx, key, token, agentClaimLockTTL).Result()
	if err != nil {
		log.ZWarn(ctx, "redis agent lock failed; falling back to local lock", err, "meetingID", meetingID)
		return func() {}, nil
	}
	if !ok {
		return nil, servererrs.ErrMeetingAgentClaimBusy.WrapMsg("another participant is currently claiming the room agent slot")
	}
	return func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Release only if still owned by this token.
		if cur, getErr := s.rdb.Get(releaseCtx, key).Result(); getErr == nil && cur == token {
			_ = s.rdb.Del(releaseCtx, key).Err()
		}
	}, nil
}

func (s *meetingServer) getPendingAgentClaim(ctx context.Context, meetingID string) *pendingAgentClaim {
	now := time.Now()
	if s.rdb != nil {
		key := "meeting:agent:pending:" + meetingID
		if raw, err := s.rdb.Get(ctx, key).Result(); err == nil && raw != "" {
			var claim pendingAgentClaim
			if json.Unmarshal([]byte(raw), &claim) == nil && claim.ExpiresAt.After(now) {
				return &claim
			}
		}
	}
	s.agentLockMu.Lock()
	defer s.agentLockMu.Unlock()
	if s.agentPendingClaims == nil {
		return nil
	}
	claim, ok := s.agentPendingClaims[meetingID]
	if !ok || !claim.ExpiresAt.After(now) {
		delete(s.agentPendingClaims, meetingID)
		return nil
	}
	copyClaim := claim
	return &copyClaim
}

func (s *meetingServer) setPendingAgentClaim(ctx context.Context, meetingID string, info agentrpc.ExistingAgentInfo) {
	claim := pendingAgentClaim{
		Info:      info,
		ExpiresAt: time.Now().Add(agentPendingClaimTTL),
	}
	s.agentLockMu.Lock()
	if s.agentPendingClaims == nil {
		s.agentPendingClaims = make(map[string]pendingAgentClaim)
	}
	s.agentPendingClaims[meetingID] = claim
	s.agentLockMu.Unlock()

	if s.rdb != nil {
		if raw, err := json.Marshal(claim); err == nil {
			_ = s.rdb.Set(ctx, "meeting:agent:pending:"+meetingID, string(raw), agentPendingClaimTTL).Err()
		}
	}
}

func (s *meetingServer) clearPendingAgentClaim(ctx context.Context, meetingID string) {
	s.agentLockMu.Lock()
	if s.agentPendingClaims != nil {
		delete(s.agentPendingClaims, meetingID)
	}
	s.agentLockMu.Unlock()
	if s.rdb != nil {
		_ = s.rdb.Del(ctx, "meeting:agent:pending:"+meetingID).Err()
	}
}

func (s *meetingServer) isMeetingHostOrCoHost(metaData *pbmeeting.MeetingMetadata, dbCreatorUserID, userID string) bool {
	if userID == "" {
		return false
	}
	if dbCreatorUserID != "" && userID == dbCreatorUserID {
		return true
	}
	if metaData != nil && metaData.Detail != nil && metaData.Detail.Info != nil {
		if metaData.Detail.Info.SystemGenerated != nil && metaData.Detail.Info.SystemGenerated.CreatorUserID == userID {
			return true
		}
		if metaData.Detail.Info.CreatorDefinedMeeting != nil {
			if metaData.Detail.Info.CreatorDefinedMeeting.HostUserID == userID {
				return true
			}
			for _, coHost := range metaData.Detail.Info.CreatorDefinedMeeting.CoHostUSerID {
				if coHost == userID {
					return true
				}
			}
		}
	}
	return false
}

func (s *meetingServer) isActiveMeetingMember(ctx context.Context, meetingID string, metaData *pbmeeting.MeetingMetadata, dbCreatorUserID, userID string) (bool, error) {
	if s.isMeetingHostOrCoHost(metaData, dbCreatorUserID, userID) {
		return true, nil
	}
	participants, err := s.meetingRtc.ListParticipants(ctx, meetingID)
	if err != nil {
		return false, err
	}
	for _, p := range participants {
		if p != nil && p.GetIdentity() == userID {
			return true, nil
		}
	}
	if metaData != nil {
		for _, pd := range metaData.PersonalData {
			if pd != nil && pd.UserID == userID {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *meetingServer) RequestAgentToken(ctx context.Context, req *agentrpc.RequestAgentTokenReq) (*agentrpc.RequestAgentTokenResp, error) {
	resp := &agentrpc.RequestAgentTokenResp{}
	if req == nil || strings.TrimSpace(req.MeetingID) == "" {
		return resp, errs.ErrArgs.WrapMsg("meetingID is required")
	}
	meetingID := strings.TrimSpace(req.MeetingID)

	userID := strings.TrimSpace(req.UserID)
	if opVal, err := common.GetKeyFromContext(ctx, sysConstant.OpUserID); err == nil {
		if opUserID, ok := opVal.(string); ok && strings.TrimSpace(opUserID) != "" {
			if userID != "" && userID != opUserID {
				return resp, servererrs.ErrMeetingAuthCheck.WrapMsg("userID does not match authenticated token user")
			}
			userID = opUserID
		}
	}
	if userID == "" {
		return resp, errs.ErrArgs.WrapMsg("userID is required")
	}

	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == "" {
		role = "translator"
	}
	if role != "transcriber" && role != "translator" {
		return resp, errs.ErrArgs.WrapMsg("invalid agent role; expected transcriber or translator")
	}

	// Verify meeting exists and is not completed.
	dbCreatorUserID := ""
	if s.meetingStorageHandler != nil {
		if dbInfo, err := s.meetingStorageHandler.TakeWithError(ctx, meetingID); err == nil && dbInfo != nil {
			dbCreatorUserID = dbInfo.CreatorUserID
			if dbInfo.Status == constant.Completed {
				return resp, servererrs.ErrMeetingAlreadyCompleted.WrapMsg("meeting has already completed")
			}
		}
	}

	// Verify live room exists in LiveKit.
	if _, err := s.meetingRtc.RoomIsExist(ctx, meetingID); err != nil {
		return resp, errs.ErrRecordNotFound.WrapMsg("active meeting room not found in LiveKit")
	}

	metaData, _ := s.meetingRtc.GetRoomData(ctx, meetingID)

	isHost := s.isMeetingHostOrCoHost(metaData, dbCreatorUserID, userID)
	if req.ForceTakeover && !isHost {
		return resp, servererrs.ErrMeetingAuthCheck.WrapMsg("only host or co-host can force takeover the room agent")
	}

	member, err := s.isActiveMeetingMember(ctx, meetingID, metaData, dbCreatorUserID, userID)
	if err != nil {
		return resp, errs.WrapMsg(err, "verify room membership failed")
	}
	if !member {
		return resp, servererrs.ErrMeetingAuthCheck.WrapMsg("only active meeting participants can request an agent token")
	}

	// Serialize concurrent claims per room.
	roomMu := s.getRoomAgentLock(meetingID)
	roomMu.Lock()
	defer roomMu.Unlock()

	releaseDistLock, err := s.acquireDistributedAgentLock(ctx, meetingID)
	if err != nil {
		return resp, err
	}
	defer releaseDistLock()

	onlineAgents, err := s.meetingRtc.ListOnlineAgents(ctx, meetingID)
	if err != nil {
		return resp, errs.WrapMsg(err, "list online agents failed")
	}

	var existing *agentrpc.ExistingAgentInfo
	if len(onlineAgents) > 0 {
		first := onlineAgents[0]
		existing = &agentrpc.ExistingAgentInfo{
			Identity:       first.Identity,
			Name:           first.Name,
			OwnerUserID:    first.OwnerUserID,
			Role:           first.Role,
			State:          first.State,
			TargetLanguage: first.TargetLanguage,
		}
	} else if pending := s.getPendingAgentClaim(ctx, meetingID); pending != nil && pending.Info.OwnerUserID != userID {
		infoCopy := pending.Info
		existing = &infoCopy
	}

	var replacedIdentity string
	if existing != nil {
		if !req.ForceTakeover {
			// Allow the same owner to seamlessly re-claim only if their previous agent is no longer online.
			resp.MeetingID = meetingID
			resp.Conflict = true
			resp.ExistingAgent = existing
			return resp, nil
		}
		// Host takeover: remove every currently online agent in the room.
		for _, a := range onlineAgents {
			if rmErr := s.meetingRtc.RemoveParticipant(ctx, meetingID, a.Identity); rmErr != nil {
				return resp, errs.WrapMsg(rmErr, "failed to remove existing room agent during takeover")
			}
			if replacedIdentity == "" {
				replacedIdentity = a.Identity
			}
		}
		if replacedIdentity == "" {
			replacedIdentity = existing.Identity
		}
		s.clearPendingAgentClaim(ctx, meetingID)
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		if s.userRpc != nil {
			if userInfo, uErr := s.userRpc.GetUserInfo(ctx, userID); uErr == nil && userInfo != nil && strings.TrimSpace(userInfo.Nickname) != "" {
				displayName = fmt.Sprintf("AI Agent (%s)", strings.TrimSpace(userInfo.Nickname))
			}
		}
		if displayName == "" {
			displayName = "AI Agent"
		}
	}

	agentIdentity := fmt.Sprintf("%s%s-%s-%s", livekit.AgentIdentityPrefix, role, sanitizeIdentitySegment(userID), randomHex(3))
	token, liveURL, expiresAt, err := s.meetingRtc.GetAgentJoinToken(ctx, rtc.AgentTokenOptions{
		RoomID:         meetingID,
		AgentIdentity:  agentIdentity,
		DisplayName:    displayName,
		OwnerUserID:    userID,
		Role:           role,
		SourceLanguage: strings.TrimSpace(req.SourceLanguage),
		TargetLanguage: strings.TrimSpace(req.TargetLanguage),
		TTLSeconds:     agentTokenTTLSeconds,
	})
	if err != nil {
		return resp, err
	}

	s.setPendingAgentClaim(ctx, meetingID, agentrpc.ExistingAgentInfo{
		Identity:       agentIdentity,
		Name:           displayName,
		OwnerUserID:    userID,
		Role:           role,
		State:          "initializing",
		TargetLanguage: strings.TrimSpace(req.TargetLanguage),
	})

	resp.MeetingID = meetingID
	resp.AgentIdentity = agentIdentity
	resp.LiveKit = &agentrpc.LiveKitInfo{
		URL:   liveURL,
		Token: token,
	}
	resp.ExpiresAtSeconds = expiresAt
	resp.ReplacedAgentIdentity = replacedIdentity
	return resp, nil
}

func (s *meetingServer) GetAgentStatus(ctx context.Context, req *agentrpc.GetAgentStatusReq) (*agentrpc.GetAgentStatusResp, error) {
	resp := &agentrpc.GetAgentStatusResp{
		Agents: []agentrpc.ExistingAgentInfo{},
	}
	if req == nil || strings.TrimSpace(req.MeetingID) == "" {
		return resp, errs.ErrArgs.WrapMsg("meetingID is required")
	}
	meetingID := strings.TrimSpace(req.MeetingID)
	resp.MeetingID = meetingID

	agents, err := s.meetingRtc.ListOnlineAgents(ctx, meetingID)
	if err != nil {
		return resp, err
	}
	for _, a := range agents {
		resp.Agents = append(resp.Agents, agentrpc.ExistingAgentInfo{
			Identity:       a.Identity,
			Name:           a.Name,
			OwnerUserID:    a.OwnerUserID,
			Role:           a.Role,
			State:          a.State,
			TargetLanguage: a.TargetLanguage,
		})
	}
	resp.HasOnlineAgent = len(resp.Agents) > 0
	return resp, nil
}

func randomHex(numBytes int) string {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func sanitizeIdentitySegment(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
		if b.Len() >= 32 {
			break
		}
	}
	if b.Len() == 0 {
		return "user"
	}
	return b.String()
}
