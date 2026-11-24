package meeting

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v4"
	lklivekit "github.com/livekit/protocol/livekit"
	"github.com/openimsdk/openmeeting-server/pkg/agentrpc"
	"github.com/openimsdk/openmeeting-server/pkg/common/config"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	"github.com/openimsdk/openmeeting-server/pkg/rtc"
	rtclivekit "github.com/openimsdk/openmeeting-server/pkg/rtc/livekit"
	pbmeeting "github.com/openimsdk/protocol/openmeeting/meeting"
	"github.com/openimsdk/tools/errs"
)

type fakeMeetingRtc struct {
	rtc.MeetingRtc
	signer       rtc.MeetingRtc
	participants []*lklivekit.ParticipantInfo
	onlineAgents []rtc.OnlineAgentInfo
	roomMetadata *pbmeeting.MeetingMetadata
	removedIDs   []string
}

func (f *fakeMeetingRtc) RoomIsExist(ctx context.Context, roomID string) (string, error) {
	return "RM_test", nil
}

func (f *fakeMeetingRtc) GetRoomData(ctx context.Context, roomID string) (*pbmeeting.MeetingMetadata, error) {
	return f.roomMetadata, nil
}

func (f *fakeMeetingRtc) ListParticipants(ctx context.Context, roomID string) ([]*lklivekit.ParticipantInfo, error) {
	return f.participants, nil
}

func (f *fakeMeetingRtc) ListOnlineAgents(ctx context.Context, roomID string) ([]rtc.OnlineAgentInfo, error) {
	return append([]rtc.OnlineAgentInfo(nil), f.onlineAgents...), nil
}

func (f *fakeMeetingRtc) RemoveParticipant(ctx context.Context, roomID, userID string) error {
	f.removedIDs = append(f.removedIDs, userID)
	next := f.onlineAgents[:0]
	for _, a := range f.onlineAgents {
		if a.Identity != userID {
			next = append(next, a)
		}
	}
	f.onlineAgents = next
	return nil
}

func (f *fakeMeetingRtc) GetAgentJoinToken(ctx context.Context, opts rtc.AgentTokenOptions) (string, string, int64, error) {
	return f.signer.GetAgentJoinToken(ctx, opts)
}

func (f *fakeMeetingRtc) CreateRoom(ctx context.Context, roomID, identify string, roomMetaData *pbmeeting.MeetingMetadata, participantMetaData *pbmeeting.ParticipantMetaData, userRpc *rpcclient.User) (string, string, string, error) {
	return "", "", "", nil
}

func newTestMeetingServer(fake *fakeMeetingRtc) *meetingServer {
	return &meetingServer{
		meetingRtc: fake,
	}
}

func TestRequestAgentToken_SuccessConflictAndHostTakeover(t *testing.T) {
	ctx := context.Background()
	lkSigner := rtclivekit.NewLiveKit(&config.RTC{
		ApiKey:    "devkey",
		ApiSecret: "secret-0123456789-abcdef-0123456789",
		URL:       []string{"wss://lk.example.com"},
		InnerURL:  "http://127.0.0.1:7880",
	})
	meta := &pbmeeting.MeetingMetadata{
		Detail: &pbmeeting.MeetingInfoSetting{
			Info: &pbmeeting.MeetingInfo{
				SystemGenerated: &pbmeeting.SystemGeneratedMeetingInfo{
					MeetingID:     "100200300",
					CreatorUserID: "host_1",
				},
				CreatorDefinedMeeting: &pbmeeting.CreatorDefinedMeetingInfo{
					HostUserID: "host_1",
				},
			},
		},
	}
	fake := &fakeMeetingRtc{
		signer:       lkSigner,
		roomMetadata: meta,
		participants: []*lklivekit.ParticipantInfo{
			{Identity: "host_1", Name: "Host"},
			{Identity: "guest_1", Name: "Guest"},
		},
	}
	srv := newTestMeetingServer(fake)

	// 1. Any active member can claim when no agent is online.
	resp, err := srv.RequestAgentToken(ctx, &agentrpc.RequestAgentTokenReq{
		MeetingID:      "100200300",
		UserID:         "guest_1",
		Role:           "translator",
		DisplayName:    "AI Translator (Guest)",
		SourceLanguage: "en",
		TargetLanguage: "zh",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Conflict || resp.LiveKit == nil || resp.LiveKit.Token == "" {
		t.Fatalf("expected valid token response, got %+v", resp)
	}

	// Verify signed LiveKit JWT structure.
	parsed, err := jwt.Parse(resp.LiveKit.Token, func(token *jwt.Token) (any, error) {
		return []byte("secret-0123456789-abcdef-0123456789"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("invalid jwt: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["kind"] != "agent" {
		t.Fatalf("expected kind=agent, got %v", claims["kind"])
	}
	video := claims["video"].(map[string]any)
	if video["canPublish"] != false || video["canSubscribe"] != false || video["canPublishData"] != true {
		t.Fatalf("unexpected video grant: %+v", video)
	}

	// Simulate the agent now being online in LiveKit.
	fake.onlineAgents = []rtc.OnlineAgentInfo{
		{
			Identity:       resp.AgentIdentity,
			Name:           "AI Translator (Guest)",
			OwnerUserID:    "guest_1",
			Role:           "translator",
			State:          "listening",
			TargetLanguage: "zh",
		},
	}

	// 2. Another participant without forceTakeover gets conflict.
	conflictResp, err := srv.RequestAgentToken(ctx, &agentrpc.RequestAgentTokenReq{
		MeetingID: "100200300",
		UserID:    "host_1",
		Role:      "translator",
	})
	if err != nil {
		t.Fatalf("unexpected conflict err: %v", err)
	}
	if !conflictResp.Conflict || conflictResp.ExistingAgent == nil || conflictResp.ExistingAgent.Identity != resp.AgentIdentity {
		t.Fatalf("expected conflict with existing agent, got %+v", conflictResp)
	}

	// 3. Non-host forceTakeover is rejected.
	_, err = srv.RequestAgentToken(ctx, &agentrpc.RequestAgentTokenReq{
		MeetingID:     "100200300",
		UserID:        "guest_1",
		Role:          "translator",
		ForceTakeover: true,
	})
	if err == nil || errs.Unwrap(err) == nil {
		t.Fatalf("expected permission error for non-host forceTakeover")
	}
	if codeErr, ok := errs.Unwrap(err).(errs.CodeError); !ok || codeErr.Code() != servererrs.MeetingAuthCheckError {
		t.Fatalf("expected MeetingAuthCheckError, got %v", err)
	}

	// 4. Host forceTakeover kicks previous agent and issues new token.
	takeoverResp, err := srv.RequestAgentToken(ctx, &agentrpc.RequestAgentTokenReq{
		MeetingID:     "100200300",
		UserID:        "host_1",
		Role:          "translator",
		ForceTakeover: true,
	})
	if err != nil {
		t.Fatalf("expected host takeover success, got %v", err)
	}
	if takeoverResp.Conflict || takeoverResp.ReplacedAgentIdentity != resp.AgentIdentity {
		t.Fatalf("expected replacement of %s, got %+v", resp.AgentIdentity, takeoverResp)
	}
	if len(fake.removedIDs) != 1 || fake.removedIDs[0] != resp.AgentIdentity {
		t.Fatalf("expected RemoveParticipant call for %s, got %+v", resp.AgentIdentity, fake.removedIDs)
	}

	// 5. GetAgentStatus reports online agents.
	fake.onlineAgents = []rtc.OnlineAgentInfo{
		{Identity: takeoverResp.AgentIdentity, Name: "AI Agent", OwnerUserID: "host_1", State: "listening"},
	}
	statusResp, err := srv.GetAgentStatus(ctx, &agentrpc.GetAgentStatusReq{MeetingID: "100200300"})
	if err != nil || !statusResp.HasOnlineAgent || len(statusResp.Agents) != 1 {
		t.Fatalf("unexpected status response: %+v, err=%v", statusResp, err)
	}
}
