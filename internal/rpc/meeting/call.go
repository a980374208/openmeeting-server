package meeting

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/openmeeting-server/pkg/callrpc"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/openmeeting-server/pkg/contactrpc"
	"github.com/openimsdk/openmeeting-server/pkg/events"
	pbmeeting "github.com/openimsdk/protocol/openmeeting/meeting"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	goredis "github.com/redis/go-redis/v9"
)

const (
	callRingingDuration = 45 * time.Second
	callSessionHardTTL  = 60 * time.Second
	inviteTicketTTL     = 5 * time.Minute
)

func callSessionKey(callID string) string {
	return "om:call:session:" + callID
}

func callActiveUserKey(userID string) string {
	return "om:call:active:" + userID
}

func meetingInviteTicketKey(meetingID, userID string) string {
	return "om:meeting:invite_ticket:" + meetingID + ":" + userID
}

// Atomic Lua script to start a direct call locking BOTH caller and callee (sorted keys)
// to prevent concurrent glare deadlocks.
var luaStartDirectCall = goredis.NewScript(`
local lock1 = KEYS[1]
local lock2 = KEYS[2]
local sessKey = KEYS[3]
if redis.call("EXISTS", lock1) == 1 or redis.call("EXISTS", lock2) == 1 then
    return 0
end
redis.call("SET", lock1, ARGV[1], "EX", tonumber(ARGV[3]))
redis.call("SET", lock2, ARGV[1], "EX", tonumber(ARGV[3]))
redis.call("SET", sessKey, ARGV[2], "EX", tonumber(ARGV[3]))
return 1
`)

// Atomic Lua script to start a meeting_invite call locking only the callee so a host
// can invite multiple contacts sequentially.
var luaStartInviteCall = goredis.NewScript(`
local calleeLock = KEYS[1]
local sessKey = KEYS[2]
if redis.call("EXISTS", calleeLock) == 1 then
    return 0
end
redis.call("SET", calleeLock, ARGV[1], "EX", tonumber(ARGV[3]))
redis.call("SET", sessKey, ARGV[2], "EX", tonumber(ARGV[3]))
return 1
`)

// Atomic Lua CAS to transition a ringing call to a terminal state and release locks.
var luaTransitionCall = goredis.NewScript(`
local sessKey = KEYS[1]
local raw = redis.call("GET", sessKey)
if not raw then
    return {0, "not_found"}
end
local sess = cjson.decode(raw)
if sess.state ~= "ringing" then
    return {0, sess.state}
end
local nowMs = tonumber(ARGV[1])
if ARGV[2] == "accepted" and nowMs > tonumber(sess.expiresAtMs) then
    sess.state = "timeout"
    redis.call("SET", sessKey, cjson.encode(sess), "EX", 30)
    if KEYS[2] ~= "" and redis.call("GET", KEYS[2]) == sess.callID then redis.call("DEL", KEYS[2]) end
    if KEYS[3] ~= "" and redis.call("GET", KEYS[3]) == sess.callID then redis.call("DEL", KEYS[3]) end
    return {0, "timeout"}
end
sess.state = ARGV[2]
redis.call("SET", sessKey, cjson.encode(sess), "EX", 30)
if KEYS[2] ~= "" and redis.call("GET", KEYS[2]) == sess.callID then redis.call("DEL", KEYS[2]) end
if KEYS[3] ~= "" and redis.call("GET", KEYS[3]) == sess.callID then redis.call("DEL", KEYS[3]) end
return {1, cjson.encode(sess)}
`)

// Local in-memory fallback store used strictly in unit tests where rdb == nil.
type memCallStore struct {
	mu       sync.Mutex
	sessions map[string]callrpc.CallSessionDTO
	active   map[string]string
	tickets  map[string]time.Time
}

var testMemCalls = &memCallStore{
	sessions: make(map[string]callrpc.CallSessionDTO),
	active:   make(map[string]string),
	tickets:  make(map[string]time.Time),
}

func (s *meetingServer) hasInviteAdmission(ctx context.Context, dbInfo *model.MeetingInfo, userID string, locked bool) bool {
	if dbInfo == nil || userID == "" {
		return false
	}
	// Active invite ticket from in-meeting host/participant invite penetrates lock.
	if s.rdb != nil {
		if val, err := s.rdb.Get(ctx, meetingInviteTicketKey(dbInfo.MeetingID, userID)).Result(); err == nil && val != "" {
			return true
		}
	} else {
		testMemCalls.mu.Lock()
		exp, ok := testMemCalls.tickets[meetingInviteTicketKey(dbInfo.MeetingID, userID)]
		testMemCalls.mu.Unlock()
		if ok && time.Now().Before(exp) {
			return true
		}
	}
	if locked {
		return false
	}
	for _, id := range dbInfo.InviteeUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

func (s *meetingServer) grantInviteTicket(ctx context.Context, meetingID, userID string) {
	key := meetingInviteTicketKey(meetingID, userID)
	if s.rdb != nil {
		_ = s.rdb.Set(ctx, key, "1", inviteTicketTTL).Err()
		return
	}
	testMemCalls.mu.Lock()
	testMemCalls.tickets[key] = time.Now().Add(inviteTicketTTL)
	testMemCalls.mu.Unlock()
}

func (s *meetingServer) StartCall(ctx context.Context, req *callrpc.StartCallReq) (*callrpc.StartCallResp, error) {
	callerID := strings.TrimSpace(req.CallerUserID)
	calleeID := strings.TrimSpace(req.CalleeUserID)
	if callerID == "" || calleeID == "" || callerID == calleeID {
		return nil, errs.ErrArgs.WrapMsg("invalid callerUserID or calleeUserID")
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = callrpc.CallModeDirect
	}
	if mode != callrpc.CallModeDirect && mode != callrpc.CallModeMeetingInvite {
		return nil, errs.ErrArgs.WrapMsg("unsupported call mode: " + mode)
	}
	mediaType := strings.TrimSpace(req.MediaType)
	if mediaType == "" {
		mediaType = callrpc.CallMediaVideo
	}
	if mediaType != callrpc.CallMediaAudio && mediaType != callrpc.CallMediaVideo {
		mediaType = callrpc.CallMediaVideo
	}

	// Check block list: if callee blocked caller, reject cleanly.
	if s.contactRpc != nil {
		if bResp, err := s.contactRpc.CheckBlocked(ctx, &contactrpc.CheckBlockedReq{
			OwnerUserID:  calleeID,
			TargetUserID: callerID,
		}); err == nil && bResp != nil && bResp.Blocked {
			return nil, servererrs.ErrContactBlocked.WrapMsg("callee is unavailable")
		}
	}

	// Check callee meeting occupancy.
	if calleeBusy, err := s.checkUserInMeeting(ctx, calleeID); err == nil && calleeBusy {
		return nil, servererrs.ErrCallBusy.WrapMsg("callee is currently in a meeting")
	}

	meetingID := strings.TrimSpace(req.MeetingID)
	meetingTitle := ""
	participantCount := 0
	if mode == callrpc.CallModeDirect {
		if callerBusy, err := s.checkUserInMeeting(ctx, callerID); err == nil && callerBusy {
			return nil, servererrs.ErrCallBusy.WrapMsg("caller is already in a meeting")
		}
	} else {
		if meetingID == "" {
			return nil, errs.ErrArgs.WrapMsg("meetingID is required for meeting_invite mode")
		}
		if s.meetingRtc != nil {
			ps, err := s.meetingRtc.ListParticipants(ctx, meetingID)
			if err != nil {
				return nil, servererrs.ErrCallNotFound.WrapMsg("active meeting room not found")
			}
			callerPresent := false
			for _, p := range ps {
				if p.Identity == callerID {
					callerPresent = true
				}
				if p.Identity == calleeID {
					return nil, servererrs.ErrCallBusy.WrapMsg("callee is already in this meeting")
				}
			}
			participantCount = len(ps)
			if !callerPresent {
				return nil, servererrs.ErrCallPermissionDenied.WrapMsg("caller is not in the target meeting")
			}
			if meta, err := s.meetingRtc.GetRoomData(ctx, meetingID); err == nil && meta != nil &&
				meta.Detail != nil && meta.Detail.Info != nil && meta.Detail.Info.CreatorDefinedMeeting != nil {
				meetingTitle = meta.Detail.Info.CreatorDefinedMeeting.Title
			}
		}
		s.grantInviteTicket(ctx, meetingID, calleeID)
	}

	callerNick := callerID
	calleeNick := calleeID
	if s.userRpc != nil {
		if uMap, err := s.userRpc.GetUsersInfoMap(ctx, []string{callerID, calleeID}); err == nil {
			if u := uMap[callerID]; u != nil && u.Nickname != "" {
				callerNick = u.Nickname
			}
			if u := uMap[calleeID]; u != nil && u.Nickname != "" {
				calleeNick = u.Nickname
			}
		}
	}

	now := time.Now()
	sess := callrpc.CallSessionDTO{
		CallID:           "call_" + uuid.New().String()[:12],
		Mode:             mode,
		MediaType:        mediaType,
		CallerUserID:     callerID,
		CallerNickname:   callerNick,
		CalleeUserID:     calleeID,
		CalleeNickname:   calleeNick,
		MeetingID:        meetingID,
		MeetingTitle:     meetingTitle,
		ParticipantCount: participantCount,
		E2EERequired:     req.E2EERequired,
		State:            callrpc.CallStateRinging,
		CreatedAtMs:      now.UnixMilli(),
		ExpiresAtMs:      now.Add(callRingingDuration).UnixMilli(),
	}

	if err := s.saveNewCallSession(ctx, sess); err != nil {
		return nil, err
	}

	if s.eventHub != nil {
		_ = s.eventHub.PublishEphemeralEvent(ctx, calleeID, events.TypeCallIncoming, sess)
	}

	// Schedule soft 45s timeout transition.
	go s.watchCallTimeout(sess.CallID, sess.CallerUserID, sess.CalleeUserID, sess.Mode, callRingingDuration)

	return &callrpc.StartCallResp{Session: sess}, nil
}

func (s *meetingServer) saveNewCallSession(ctx context.Context, sess callrpc.CallSessionDTO) error {
	raw, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	if s.rdb != nil {
		ttlSec := int(callSessionHardTTL.Seconds())
		var ok int
		if sess.Mode == callrpc.CallModeDirect {
			pair := []string{sess.CallerUserID, sess.CalleeUserID}
			sort.Strings(pair)
			ok, err = luaStartDirectCall.Run(ctx, s.rdb,
				[]string{callActiveUserKey(pair[0]), callActiveUserKey(pair[1]), callSessionKey(sess.CallID)},
				sess.CallID, string(raw), ttlSec,
			).Int()
		} else {
			ok, err = luaStartInviteCall.Run(ctx, s.rdb,
				[]string{callActiveUserKey(sess.CalleeUserID), callSessionKey(sess.CallID)},
				sess.CallID, string(raw), ttlSec,
			).Int()
		}
		if err != nil {
			return errs.Wrap(err)
		}
		if ok == 0 {
			return servererrs.ErrCallBusy.WrapMsg("user is busy with another call")
		}
		return nil
	}

	// In-memory unit-test path with sorted key locking semantics.
	testMemCalls.mu.Lock()
	defer testMemCalls.mu.Unlock()
	if sess.Mode == callrpc.CallModeDirect {
		if _, busy := testMemCalls.active[sess.CallerUserID]; busy {
			return servererrs.ErrCallBusy.WrapMsg("caller is busy with another call")
		}
	}
	if _, busy := testMemCalls.active[sess.CalleeUserID]; busy {
		return servererrs.ErrCallBusy.WrapMsg("callee is busy with another call")
	}
	if sess.Mode == callrpc.CallModeDirect {
		testMemCalls.active[sess.CallerUserID] = sess.CallID
	}
	testMemCalls.active[sess.CalleeUserID] = sess.CallID
	testMemCalls.sessions[sess.CallID] = sess
	return nil
}

func (s *meetingServer) transitionCallState(ctx context.Context, callID, targetState string) (*callrpc.CallSessionDTO, string, error) {
	if s.rdb != nil {
		// Fetch session first to know lock keys.
		raw, err := s.rdb.Get(ctx, callSessionKey(callID)).Result()
		if err == goredis.Nil {
			return nil, "not_found", servererrs.ErrCallNotFound.WrapMsg("call session not found")
		}
		if err != nil {
			return nil, "", errs.Wrap(err)
		}
		var preview callrpc.CallSessionDTO
		if err := json.Unmarshal([]byte(raw), &preview); err != nil {
			return nil, "", errs.Wrap(err)
		}
		lock1 := ""
		if preview.Mode == callrpc.CallModeDirect {
			lock1 = callActiveUserKey(preview.CallerUserID)
		}
		lock2 := callActiveUserKey(preview.CalleeUserID)
		res, err := luaTransitionCall.Run(ctx, s.rdb,
			[]string{callSessionKey(callID), lock1, lock2},
			time.Now().UnixMilli(), targetState,
		).Slice()
		if err != nil {
			return nil, "", errs.Wrap(err)
		}
		code, _ := res[0].(int64)
		detail, _ := res[1].(string)
		if code == 0 {
			if detail == "not_found" {
				return nil, detail, servererrs.ErrCallNotFound.WrapMsg("call session not found")
			}
			return &preview, detail, servererrs.ErrCallAlreadyEnded.WrapMsg("call already ended: " + detail)
		}
		var updated callrpc.CallSessionDTO
		if err := json.Unmarshal([]byte(detail), &updated); err != nil {
			return nil, "", errs.Wrap(err)
		}
		return &updated, updated.State, nil
	}

	// In-memory unit-test path
	testMemCalls.mu.Lock()
	defer testMemCalls.mu.Unlock()
	sess, ok := testMemCalls.sessions[callID]
	if !ok {
		return nil, "not_found", servererrs.ErrCallNotFound.WrapMsg("call session not found")
	}
	if sess.State != callrpc.CallStateRinging {
		return &sess, sess.State, servererrs.ErrCallAlreadyEnded.WrapMsg("call already ended: " + sess.State)
	}
	if targetState == callrpc.CallStateAccepted && time.Now().UnixMilli() > sess.ExpiresAtMs {
		sess.State = callrpc.CallStateTimeout
		testMemCalls.sessions[callID] = sess
		if testMemCalls.active[sess.CallerUserID] == callID {
			delete(testMemCalls.active, sess.CallerUserID)
		}
		if testMemCalls.active[sess.CalleeUserID] == callID {
			delete(testMemCalls.active, sess.CalleeUserID)
		}
		return &sess, sess.State, servererrs.ErrCallAlreadyEnded.WrapMsg("call timed out")
	}
	sess.State = targetState
	testMemCalls.sessions[callID] = sess
	if testMemCalls.active[sess.CallerUserID] == callID {
		delete(testMemCalls.active, sess.CallerUserID)
	}
	if testMemCalls.active[sess.CalleeUserID] == callID {
		delete(testMemCalls.active, sess.CalleeUserID)
	}
	return &sess, sess.State, nil
}

func (s *meetingServer) watchCallTimeout(callID, callerID, calleeID, mode string, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	<-timer.C
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, _, err := s.transitionCallState(ctx, callID, callrpc.CallStateTimeout)
	if err != nil || sess == nil {
		return
	}
	if s.eventHub != nil {
		_ = s.eventHub.PublishEphemeralEvent(ctx, callerID, events.TypeCallTimeout, sess)
		_ = s.eventHub.PublishEphemeralEvent(ctx, calleeID, events.TypeCallTimeout, sess)
	}
}

func (s *meetingServer) AcceptCall(ctx context.Context, req *callrpc.AcceptCallReq) (*callrpc.AcceptCallResp, error) {
	callID := strings.TrimSpace(req.CallID)
	calleeID := strings.TrimSpace(req.CalleeUserID)
	if callID == "" || calleeID == "" {
		return nil, errs.ErrArgs.WrapMsg("callID and calleeUserID are required")
	}

	// Verify callee ownership before state transition.
	preview, err := s.getCallSession(ctx, callID)
	if err != nil {
		return nil, err
	}
	if preview.CalleeUserID != calleeID {
		return nil, servererrs.ErrCallPermissionDenied.WrapMsg("only callee can accept this call")
	}

	sess, _, err := s.transitionCallState(ctx, callID, callrpc.CallStateAccepted)
	if err != nil {
		return nil, err
	}

	var calleeCreds callrpc.LiveKitCredentials
	var callerCreds *callrpc.LiveKitCredentials

	if sess.Mode == callrpc.CallModeDirect {
		// Lazy Room Creation: create room & DB meeting ONLY when callee actually accepts.
		createReq := &pbmeeting.CreateImmediateMeetingReq{
			CreatorUserID: sess.CallerUserID,
			CreatorDefinedMeetingInfo: &pbmeeting.CreatorDefinedMeetingInfo{
				Title:           buildDirectCallTitle(sess.CallerNickname, sess.CalleeNickname),
				ScheduledTime:   time.Now().Unix(),
				MeetingDuration: 3600,
			},
			Setting: &pbmeeting.MeetingSetting{
				CanParticipantsEnableCamera:     true,
				CanParticipantsUnmuteMicrophone: true,
				CanParticipantsShareScreen:      true,
				DisableCameraOnJoin:             sess.MediaType == callrpc.CallMediaAudio,
				DisableMicrophoneOnJoin:         false,
			},
		}
		createResp, err := s.CreateImmediateMeeting(ctx, createReq)
		if err != nil {
			return nil, err
		}
		meetingID := createResp.Detail.Info.SystemGenerated.MeetingID
		sess.MeetingID = meetingID
		sess.MeetingTitle = createReq.CreatorDefinedMeetingInfo.Title
		// Persist callee in invitee_user_ids for schedule/history visibility.
		if s.meetingStorageHandler != nil {
			_ = s.meetingStorageHandler.Update(ctx, meetingID, map[string]any{
				"invitee_user_ids": []string{sess.CalleeUserID},
			})
		}
		// Issue callee LiveKit join token directly so callee joins in 1 step.
		calleeUserInfo, uErr := s.userRpc.GetUserInfo(ctx, sess.CalleeUserID)
		if uErr != nil {
			return nil, errs.WrapMsg(uErr, "get callee user info failed")
		}
		calleeToken, liveURL, tErr := s.meetingRtc.GetJoinToken(ctx, meetingID, sess.CalleeUserID, s.generateParticipantMetaData(calleeUserInfo), false)
		if tErr != nil {
			return nil, errs.WrapMsg(tErr, "issue callee join token failed")
		}
		if s.eventHub != nil {
			_ = s.eventHub.SetUserInMeeting(ctx, sess.CalleeUserID, meetingID)
		}
		calleeCreds = callrpc.LiveKitCredentials{
			URL:       liveURL,
			Token:     calleeToken,
			MeetingID: meetingID,
		}
		callerCreds = &callrpc.LiveKitCredentials{
			URL:       createResp.LiveKit.Url,
			Token:     createResp.LiveKit.Token,
			MeetingID: meetingID,
		}
	} else {
		// meeting_invite: issue passwordless join token for existing active room.
		calleeUserInfo, uErr := s.userRpc.GetUserInfo(ctx, sess.CalleeUserID)
		if uErr != nil {
			return nil, errs.WrapMsg(uErr, "get callee user info failed")
		}
		calleeToken, liveURL, tErr := s.meetingRtc.GetJoinToken(ctx, sess.MeetingID, sess.CalleeUserID, s.generateParticipantMetaData(calleeUserInfo), false)
		if tErr != nil {
			return nil, errs.WrapMsg(tErr, "issue invite join token failed")
		}
		if s.eventHub != nil {
			_ = s.eventHub.SetUserInMeeting(ctx, sess.CalleeUserID, sess.MeetingID)
		}
		calleeCreds = callrpc.LiveKitCredentials{
			URL:       liveURL,
			Token:     calleeToken,
			MeetingID: sess.MeetingID,
		}
	}

	if s.eventHub != nil {
		payload := map[string]any{
			"session": sess,
		}
		if callerCreds != nil {
			payload["liveKit"] = callerCreds
		}
		_ = s.eventHub.PublishEphemeralEvent(ctx, sess.CallerUserID, events.TypeCallAccepted, payload)
	}

	return &callrpc.AcceptCallResp{
		Session: *sess,
		LiveKit: calleeCreds,
	}, nil
}

func (s *meetingServer) RejectCall(ctx context.Context, req *callrpc.RejectCallReq) (*callrpc.RejectCallResp, error) {
	callID := strings.TrimSpace(req.CallID)
	calleeID := strings.TrimSpace(req.CalleeUserID)
	if callID == "" || calleeID == "" {
		return nil, errs.ErrArgs.WrapMsg("callID and calleeUserID are required")
	}
	preview, err := s.getCallSession(ctx, callID)
	if err != nil {
		return nil, err
	}
	if preview.CalleeUserID != calleeID {
		return nil, servererrs.ErrCallPermissionDenied.WrapMsg("only callee can reject this call")
	}
	targetState := callrpc.CallStateRejected
	switch strings.ToLower(strings.TrimSpace(req.Reason)) {
	case callrpc.CallStateBusy:
		targetState = callrpc.CallStateBusy
	case callrpc.CallStateTimeout:
		targetState = callrpc.CallStateTimeout
	}
	sess, state, err := s.transitionCallState(ctx, callID, targetState)
	if err != nil {
		return nil, err
	}
	if s.eventHub != nil {
		_ = s.eventHub.PublishEphemeralEvent(ctx, sess.CallerUserID, events.TypeCallRejected, sess)
	}
	return &callrpc.RejectCallResp{CallID: callID, State: state}, nil
}

func (s *meetingServer) CancelCall(ctx context.Context, req *callrpc.CancelCallReq) (*callrpc.CancelCallResp, error) {
	callID := strings.TrimSpace(req.CallID)
	callerID := strings.TrimSpace(req.CallerUserID)
	if callID == "" || callerID == "" {
		return nil, errs.ErrArgs.WrapMsg("callID and callerUserID are required")
	}
	preview, err := s.getCallSession(ctx, callID)
	if err != nil {
		return nil, err
	}
	if preview.CallerUserID != callerID {
		return nil, servererrs.ErrCallPermissionDenied.WrapMsg("only caller can cancel this call")
	}
	targetState := callrpc.CallStateCancelled
	if strings.EqualFold(strings.TrimSpace(req.Reason), callrpc.CallStateTimeout) {
		targetState = callrpc.CallStateTimeout
	}
	sess, state, err := s.transitionCallState(ctx, callID, targetState)
	if err != nil {
		return nil, err
	}
	if s.eventHub != nil {
		evtType := events.TypeCallCancelled
		if targetState == callrpc.CallStateTimeout {
			evtType = events.TypeCallTimeout
		}
		_ = s.eventHub.PublishEphemeralEvent(ctx, sess.CalleeUserID, evtType, sess)
	}
	return &callrpc.CancelCallResp{CallID: callID, State: state}, nil
}

func (s *meetingServer) GetActiveIncomingCall(ctx context.Context, req *callrpc.GetActiveIncomingCallReq) (*callrpc.GetActiveIncomingCallResp, error) {
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return &callrpc.GetActiveIncomingCallResp{}, nil
	}
	var callID string
	if s.rdb != nil {
		val, err := s.rdb.Get(ctx, callActiveUserKey(userID)).Result()
		if err != nil || val == "" {
			return &callrpc.GetActiveIncomingCallResp{}, nil
		}
		callID = val
	} else {
		testMemCalls.mu.Lock()
		callID = testMemCalls.active[userID]
		testMemCalls.mu.Unlock()
		if callID == "" {
			return &callrpc.GetActiveIncomingCallResp{}, nil
		}
	}
	sess, err := s.getCallSession(ctx, callID)
	if err != nil || sess == nil {
		return &callrpc.GetActiveIncomingCallResp{}, nil
	}
	if sess.CalleeUserID != userID || sess.State != callrpc.CallStateRinging || time.Now().UnixMilli() > sess.ExpiresAtMs {
		return &callrpc.GetActiveIncomingCallResp{}, nil
	}
	return &callrpc.GetActiveIncomingCallResp{Session: sess}, nil
}

func (s *meetingServer) getCallSession(ctx context.Context, callID string) (*callrpc.CallSessionDTO, error) {
	if s.rdb != nil {
		raw, err := s.rdb.Get(ctx, callSessionKey(callID)).Result()
		if err == goredis.Nil {
			return nil, servererrs.ErrCallNotFound.WrapMsg("call session not found")
		}
		if err != nil {
			return nil, errs.Wrap(err)
		}
		var sess callrpc.CallSessionDTO
		if err := json.Unmarshal([]byte(raw), &sess); err != nil {
			return nil, errs.Wrap(err)
		}
		return &sess, nil
	}
	testMemCalls.mu.Lock()
	defer testMemCalls.mu.Unlock()
	sess, ok := testMemCalls.sessions[callID]
	if !ok {
		return nil, servererrs.ErrCallNotFound.WrapMsg("call session not found")
	}
	return &sess, nil
}

func (s *meetingServer) SetMeetingInvitees(ctx context.Context, req *callrpc.SetMeetingInviteesReq) (*callrpc.SetMeetingInviteesResp, error) {
	meetingID := strings.TrimSpace(req.MeetingID)
	if meetingID == "" {
		return nil, errs.ErrArgs.WrapMsg("meetingID is required")
	}
	info, err := s.meetingStorageHandler.TakeWithError(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	if req.OperatorUserID != "" && info.CreatorUserID != req.OperatorUserID {
		return nil, servererrs.ErrMeetingAuthCheck.WrapMsg("only creator can manage meeting invitees")
	}
	seen := make(map[string]struct{}, len(req.InviteeUserIDs))
	cleaned := make([]string, 0, len(req.InviteeUserIDs))
	prevSet := make(map[string]struct{}, len(info.InviteeUserIDs))
	for _, id := range info.InviteeUserIDs {
		prevSet[id] = struct{}{}
	}
	for _, id := range req.InviteeUserIDs {
		id = strings.TrimSpace(id)
		if id == "" || id == info.CreatorUserID {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			cleaned = append(cleaned, id)
		}
	}
	if err := s.meetingStorageHandler.Update(ctx, meetingID, map[string]any{
		"invitee_user_ids": cleaned,
	}); err != nil {
		return nil, err
	}
	// Notify newly added invitees via durable event inbox.
	if s.eventHub != nil {
		title := req.MeetingTitle
		if title == "" {
			title = info.Title
		}
		sched := req.ScheduledTime
		if sched == 0 {
			sched = info.ScheduledTime
		}
		for _, uid := range cleaned {
			if _, existed := prevSet[uid]; !existed {
				_, _ = s.eventHub.PublishDurableEvent(ctx, uid, events.TypeMeetingInvited, map[string]any{
					"meetingID":     meetingID,
					"title":         title,
					"creatorUserID": info.CreatorUserID,
					"scheduledTime": sched,
				})
			}
		}
	}
	return &callrpc.SetMeetingInviteesResp{
		MeetingID:      meetingID,
		InviteeUserIDs: cleaned,
	}, nil
}

func (s *meetingServer) GetMeetingInviteesBatch(ctx context.Context, req *callrpc.GetMeetingInviteesBatchReq) (*callrpc.GetMeetingInviteesBatchResp, error) {
	out := make(map[string][]string, len(req.MeetingIDs))
	if len(req.MeetingIDs) == 0 || s.meetingStorageHandler == nil {
		return &callrpc.GetMeetingInviteesBatchResp{Invitees: out}, nil
	}
	meetings, err := s.meetingStorageHandler.FindByMeetingIDs(ctx, req.MeetingIDs)
	if err != nil {
		return nil, err
	}
	for _, m := range meetings {
		if len(m.InviteeUserIDs) > 0 {
			out[m.MeetingID] = append([]string(nil), m.InviteeUserIDs...)
		}
	}
	return &callrpc.GetMeetingInviteesBatchResp{Invitees: out}, nil
}

func (s *meetingServer) HandleLiveKitWebhookEvent(ctx context.Context, req *callrpc.LiveKitWebhookEventReq) (*callrpc.LiveKitWebhookEventResp, error) {
	if s.eventHub == nil {
		return &callrpc.LiveKitWebhookEventResp{Handled: false}, nil
	}
	eventName := strings.TrimSpace(req.Event)
	roomName := strings.TrimSpace(req.RoomName)
	identity := strings.TrimSpace(req.ParticipantIdentity)
	// Ignore AI translation/transcription bot identities.
	if strings.HasPrefix(identity, "agent_") || strings.HasPrefix(identity, "bot_") {
		return &callrpc.LiveKitWebhookEventResp{Handled: true}, nil
	}
	switch eventName {
	case "participant_joined":
		if identity != "" && roomName != "" {
			_ = s.eventHub.SetUserInMeeting(ctx, identity, roomName)
		}
		return &callrpc.LiveKitWebhookEventResp{Handled: true}, nil
	case "participant_left":
		if identity != "" {
			_ = s.eventHub.ClearUserInMeeting(ctx, identity, roomName)
		}
		return &callrpc.LiveKitWebhookEventResp{Handled: true}, nil
	case "room_finished":
		if roomName != "" && s.meetingStorageHandler != nil {
			if info, err := s.meetingStorageHandler.TakeCatalog(ctx, roomName); err == nil && info != nil {
				_ = s.eventHub.ClearUserInMeeting(ctx, info.CreatorUserID, roomName)
				for _, uid := range info.InviteeUserIDs {
					_ = s.eventHub.ClearUserInMeeting(ctx, uid, roomName)
				}
			}
		}
		log.ZDebug(ctx, "livekit webhook room_finished processed", "room", roomName)
		return &callrpc.LiveKitWebhookEventResp{Handled: true}, nil
	default:
		return &callrpc.LiveKitWebhookEventResp{Handled: false}, nil
	}
}

func buildDirectCallTitle(callerName, calleeName string) string {
	callerName = strings.TrimSpace(callerName)
	calleeName = strings.TrimSpace(calleeName)
	if callerName == "" {
		return "Quick Call"
	}
	if calleeName == "" {
		return callerName + "'s Call"
	}
	return callerName + " & " + calleeName
}
