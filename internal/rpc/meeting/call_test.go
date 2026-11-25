package meeting

import (
	"context"
	"testing"
	"time"

	lklivekit "github.com/livekit/protocol/livekit"
	"github.com/openimsdk/openmeeting-server/pkg/callrpc"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/openmeeting-server/pkg/contactrpc"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	pbmeeting "github.com/openimsdk/protocol/openmeeting/meeting"
	pbuser "github.com/openimsdk/protocol/openmeeting/user"
	"github.com/openimsdk/tools/errs"
	"google.golang.org/grpc"
)

type fakeCallMeetingStorage struct {
	meetings map[string]*model.MeetingInfo
	seq      int
}

func (f *fakeCallMeetingStorage) TakeWithError(ctx context.Context, meetingID string) (*model.MeetingInfo, error) {
	m, ok := f.meetings[meetingID]
	if !ok {
		return nil, errs.ErrRecordNotFound.WrapMsg("not found")
	}
	return m, nil
}
func (f *fakeCallMeetingStorage) Create(ctx context.Context, meetings []*model.MeetingInfo) error {
	for _, m := range meetings {
		f.meetings[m.MeetingID] = m
	}
	return nil
}
func (f *fakeCallMeetingStorage) Update(ctx context.Context, meetingID string, updateData map[string]any) error {
	m, ok := f.meetings[meetingID]
	if !ok {
		return errs.ErrRecordNotFound.WrapMsg("not found")
	}
	if inv, ok := updateData["invitee_user_ids"].([]string); ok {
		m.InviteeUserIDs = inv
	}
	return nil
}
func (f *fakeCallMeetingStorage) Delete(ctx context.Context, meetingID string) error {
	delete(f.meetings, meetingID)
	return nil
}
func (f *fakeCallMeetingStorage) FindByStatus(ctx context.Context, status []string, userID string) ([]*model.MeetingInfo, error) {
	return nil, nil
}
func (f *fakeCallMeetingStorage) FindByMeetingIDs(ctx context.Context, meetingIDs []string) ([]*model.MeetingInfo, error) {
	var out []*model.MeetingInfo
	for _, id := range meetingIDs {
		if m, ok := f.meetings[id]; ok {
			out = append(out, m)
		}
	}
	return out, nil
}
func (f *fakeCallMeetingStorage) TakeCatalog(ctx context.Context, meetingID string) (*model.MeetingInfo, error) {
	return f.TakeWithError(ctx, meetingID)
}
func (f *fakeCallMeetingStorage) GenerateMeetingID(ctx context.Context) (string, error) {
	f.seq++
	return "900000001", nil
}

type fakeCallUserFinder struct {
	users map[string]*pbuser.UserInfo
}

func (f *fakeCallUserFinder) GetUsersInfos(ctx context.Context, userIDs []string) ([]*pbuser.UserInfo, error) {
	var out []*pbuser.UserInfo
	for _, id := range userIDs {
		if u, ok := f.users[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}

type fakeContactClient struct {
	contactrpc.ContactDirectoryServiceClient
	blocked map[string]bool // "owner:target"
}

func (f *fakeContactClient) CheckBlocked(ctx context.Context, req *contactrpc.CheckBlockedReq, opts ...grpc.CallOption) (*contactrpc.CheckBlockedResp, error) {
	return &contactrpc.CheckBlockedResp{Blocked: f.blocked[req.OwnerUserID+":"+req.TargetUserID]}, nil
}

type fakeCallRtc struct {
	fakeMeetingRtc
	createRoomCount int
	rooms           []*lklivekit.Room
}

func (f *fakeCallRtc) GetAllRooms(ctx context.Context) ([]*lklivekit.Room, error) {
	return f.rooms, nil
}
func (f *fakeCallRtc) GetParticipantUserIDs(ctx context.Context, roomID string) ([]string, error) {
	var out []string
	for _, p := range f.participants {
		out = append(out, p.Identity)
	}
	return out, nil
}
func (f *fakeCallRtc) CreateRoom(ctx context.Context, roomID, identify string, roomMetaData *pbmeeting.MeetingMetadata, participantMetaData *pbmeeting.ParticipantMetaData, userRpc *rpcclient.User) (string, string, string, error) {
	f.createRoomCount++
	return "RM_" + roomID, "tok_caller_" + identify, "wss://lk.test", nil
}
func (f *fakeCallRtc) GetJoinToken(ctx context.Context, roomID, identity string, metadata *pbmeeting.ParticipantMetaData, isListener bool) (string, string, error) {
	return "tok_" + identity, "wss://lk.test", nil
}
func (f *fakeCallRtc) UpdateMetaData(ctx context.Context, updateData *pbmeeting.MeetingMetadata) error {
	return nil
}

func resetMemCalls() {
	testMemCalls.mu.Lock()
	testMemCalls.sessions = make(map[string]callrpc.CallSessionDTO)
	testMemCalls.active = make(map[string]string)
	testMemCalls.tickets = make(map[string]time.Time)
	testMemCalls.mu.Unlock()
}

func TestDirectCallLazyRoomCreationAndGlare(t *testing.T) {
	resetMemCalls()
	ctx := context.Background()
	store := &fakeCallMeetingStorage{meetings: make(map[string]*model.MeetingInfo)}
	rtcFake := &fakeCallRtc{}
	contactFake := &fakeContactClient{blocked: map[string]bool{"u_blocked:u_alice": true}}
	userFinder := &fakeCallUserFinder{
		users: map[string]*pbuser.UserInfo{
			"u_alice":   {UserID: "u_alice", Nickname: "Alice"},
			"u_bob":     {UserID: "u_bob", Nickname: "Bob"},
			"u_blocked": {UserID: "u_blocked", Nickname: "Blocked"},
		},
	}

	srv := &meetingServer{
		meetingStorageHandler: store,
		meetingRtc:            rtcFake,
		userRpc:               rpcclient.NewUser(userFinder),
		contactRpc:            contactFake,
	}

	// 1. Blocked target rejects immediately without creating a room.
	if _, err := srv.StartCall(ctx, &callrpc.StartCallReq{
		CallerUserID: "u_alice",
		CalleeUserID: "u_blocked",
		Mode:         callrpc.CallModeDirect,
	}); err == nil {
		t.Fatalf("expected blocked error")
	}

	// 2. Start direct call: must NOT allocate a LiveKit room yet (Lazy Room Creation).
	startResp, err := srv.StartCall(ctx, &callrpc.StartCallReq{
		CallerUserID: "u_alice",
		CalleeUserID: "u_bob",
		Mode:         callrpc.CallModeDirect,
		MediaType:    callrpc.CallMediaAudio,
	})
	if err != nil {
		t.Fatalf("StartCall failed: %v", err)
	}
	if rtcFake.createRoomCount != 0 || len(store.meetings) != 0 {
		t.Fatalf("expected 0 rooms before accept, got rooms=%d meetings=%d", rtcFake.createRoomCount, len(store.meetings))
	}

	// 3. Glare: Bob simultaneously calling Alice must return ErrCallBusy.
	if _, err := srv.StartCall(ctx, &callrpc.StartCallReq{
		CallerUserID: "u_bob",
		CalleeUserID: "u_alice",
		Mode:         callrpc.CallModeDirect,
	}); err == nil {
		t.Fatalf("expected busy error on concurrent glare call")
	} else if codeErr, ok := errs.Unwrap(err).(errs.CodeError); !ok || codeErr.Code() != servererrs.CallBusyError {
		t.Fatalf("expected CallBusyError, got %v", err)
	}

	// 4. Reconnect lookup returns active ringing call for Bob.
	activeResp, err := srv.GetActiveIncomingCall(ctx, &callrpc.GetActiveIncomingCallReq{UserID: "u_bob"})
	if err != nil || activeResp.Session == nil || activeResp.Session.CallID != startResp.Session.CallID {
		t.Fatalf("expected active incoming call %s, got %+v", startResp.Session.CallID, activeResp)
	}

	// 5. Bob accepts -> lazily creates room once and returns callee credentials.
	accResp, err := srv.AcceptCall(ctx, &callrpc.AcceptCallReq{
		CallID:       startResp.Session.CallID,
		CalleeUserID: "u_bob",
	})
	if err != nil {
		t.Fatalf("AcceptCall failed: %v", err)
	}
	if rtcFake.createRoomCount != 1 {
		t.Fatalf("expected 1 lazily created room, got %d", rtcFake.createRoomCount)
	}
	if accResp.LiveKit.MeetingID == "" || accResp.LiveKit.Token == "" {
		t.Fatalf("expected livekit credentials on accept, got %+v", accResp.LiveKit)
	}

	// 6. Second accept/cancel after acceptance must fail with CallAlreadyEnded.
	if _, err := srv.CancelCall(ctx, &callrpc.CancelCallReq{
		CallID:       startResp.Session.CallID,
		CallerUserID: "u_alice",
	}); err == nil {
		t.Fatalf("expected error canceling already accepted call")
	}
}
