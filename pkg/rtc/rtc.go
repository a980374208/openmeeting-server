package rtc

import (
	"context"
	"github.com/livekit/protocol/livekit"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	"github.com/openimsdk/protocol/openmeeting/meeting"
)

type AgentTokenOptions struct {
	RoomID         string
	AgentIdentity  string
	DisplayName    string
	OwnerUserID    string
	Role           string
	SourceLanguage string
	TargetLanguage string
	TTLSeconds     int64
}

type OnlineAgentInfo struct {
	Identity       string
	Name           string
	OwnerUserID    string
	Role           string
	State          string
	TargetLanguage string
}

type MeetingRtc interface {
	GetJoinToken(ctx context.Context, roomID, identity string, metadata *meeting.ParticipantMetaData, isListener bool) (string, string, error)
	GetAgentJoinToken(ctx context.Context, opts AgentTokenOptions) (token string, liveURL string, expiresAt int64, err error)
	ListOnlineAgents(ctx context.Context, roomID string) ([]OnlineAgentInfo, error)
	CreateRoom(ctx context.Context, roomID, identify string, roomMetaData *meeting.MeetingMetadata, participantMetaData *meeting.ParticipantMetaData, userRpc *rpcclient.User) (sID, token, liveUrl string, err error)
	GetRoomData(ctx context.Context, roomID string) (*meeting.MeetingMetadata, error)
	GetAllRooms(ctx context.Context) ([]*livekit.Room, error)
	GetRoom(ctx context.Context, roomID string) (*livekit.Room, error)
	RoomIsExist(ctx context.Context, roomID string) (string, error)
	UpdateMetaData(ctx context.Context, info *meeting.MeetingMetadata) error
	CloseRoom(ctx context.Context, roomID string) error
	RemoveParticipant(ctx context.Context, roomID, userID string) error
	ToggleMimeStream(ctx context.Context, roomID, userID, mineType string, mute bool) error
	SendRoomData(ctx context.Context, roomID string, userIDList *[]string, sendData *meeting.NotifyMeetingData) error
	ListParticipants(ctx context.Context, roomID string) ([]*livekit.ParticipantInfo, error)
	GetParticipantUserIDs(ctx context.Context, roomID string) ([]string, error)
	UpdateParticipantData(ctx context.Context, data *meeting.ParticipantMetaData, roomID, userID string) error
	GetParticipantMetaData(ctx context.Context, roomID, userID string) (*meeting.ParticipantMetaData, error)
}
