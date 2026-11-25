package callrpc

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	serviceName                     = "openmeeting.meeting.CallSignalingService"
	methodStartCall                 = "/" + serviceName + "/StartCall"
	methodAcceptCall                = "/" + serviceName + "/AcceptCall"
	methodRejectCall                = "/" + serviceName + "/RejectCall"
	methodCancelCall                = "/" + serviceName + "/CancelCall"
	methodGetActiveIncomingCall     = "/" + serviceName + "/GetActiveIncomingCall"
	methodSetMeetingInvitees        = "/" + serviceName + "/SetMeetingInvitees"
	methodGetMeetingInviteesBatch   = "/" + serviceName + "/GetMeetingInviteesBatch"
	methodHandleLiveKitWebhookEvent = "/" + serviceName + "/HandleLiveKitWebhookEvent"
)

const (
	CallModeDirect        = "direct"
	CallModeMeetingInvite = "meeting_invite"

	CallMediaAudio = "audio"
	CallMediaVideo = "video"

	CallStateRinging   = "ringing"
	CallStateAccepted  = "accepted"
	CallStateRejected  = "rejected"
	CallStateBusy      = "busy"
	CallStateCancelled = "cancelled"
	CallStateTimeout   = "timeout"
)

type LiveKitCredentials struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	MeetingID string `json:"meetingID"`
}

type CallSessionDTO struct {
	CallID           string `json:"callID"`
	Mode             string `json:"mode"`      // "direct" | "meeting_invite"
	MediaType        string `json:"mediaType"` // "audio" | "video"
	CallerUserID     string `json:"callerUserID"`
	CallerNickname   string `json:"callerNickname"`
	CalleeUserID     string `json:"calleeUserID"`
	CalleeNickname   string `json:"calleeNickname,omitempty"`
	MeetingID        string `json:"meetingID,omitempty"`
	MeetingTitle     string `json:"meetingTitle,omitempty"`
	ParticipantCount int    `json:"participantCount,omitempty"`
	E2EERequired     bool   `json:"e2eeRequired"`
	State            string `json:"state"`
	CreatedAtMs      int64  `json:"createdAtMs"`
	ExpiresAtMs      int64  `json:"expiresAtMs"`
}

type StartCallReq struct {
	CallerUserID string `json:"callerUserID"`
	CalleeUserID string `json:"calleeUserID"`
	Mode         string `json:"mode,omitempty"`      // default "direct"
	MediaType    string `json:"mediaType,omitempty"` // default "video"
	MeetingID    string `json:"meetingID,omitempty"` // required when mode == "meeting_invite"
	E2EERequired bool   `json:"e2eeRequired,omitempty"`
}

type StartCallResp struct {
	Session CallSessionDTO `json:"session"`
}

type AcceptCallReq struct {
	CallID       string `json:"callID"`
	CalleeUserID string `json:"calleeUserID"`
}

type AcceptCallResp struct {
	Session CallSessionDTO     `json:"session"`
	LiveKit LiveKitCredentials `json:"liveKit"`
}

type RejectCallReq struct {
	CallID       string `json:"callID"`
	CalleeUserID string `json:"calleeUserID"`
	Reason       string `json:"reason,omitempty"` // "rejected" | "busy" | "timeout"
}

type RejectCallResp struct {
	CallID string `json:"callID"`
	State  string `json:"state"`
}

type CancelCallReq struct {
	CallID       string `json:"callID"`
	CallerUserID string `json:"callerUserID"`
	Reason       string `json:"reason,omitempty"` // "cancelled" | "timeout"
}

type CancelCallResp struct {
	CallID string `json:"callID"`
	State  string `json:"state"`
}

type GetActiveIncomingCallReq struct {
	UserID string `json:"userID"`
}

type GetActiveIncomingCallResp struct {
	Session *CallSessionDTO `json:"session,omitempty"`
}

type SetMeetingInviteesReq struct {
	MeetingID      string   `json:"meetingID"`
	OperatorUserID string   `json:"operatorUserID"`
	InviteeUserIDs []string `json:"inviteeUserIDs"`
	MeetingTitle   string   `json:"meetingTitle,omitempty"`
	ScheduledTime  int64    `json:"scheduledTime,omitempty"`
}

type SetMeetingInviteesResp struct {
	MeetingID      string   `json:"meetingID"`
	InviteeUserIDs []string `json:"inviteeUserIDs"`
}

type GetMeetingInviteesBatchReq struct {
	MeetingIDs []string `json:"meetingIDs"`
}

type GetMeetingInviteesBatchResp struct {
	Invitees map[string][]string `json:"invitees"`
}

type LiveKitWebhookEventReq struct {
	Event               string `json:"event"` // participant_joined | participant_left | room_finished
	RoomName            string `json:"roomName"`
	ParticipantIdentity string `json:"participantIdentity,omitempty"`
}

type LiveKitWebhookEventResp struct {
	Handled bool `json:"handled"`
}

type CallSignalingServiceServer interface {
	StartCall(ctx context.Context, req *StartCallReq) (*StartCallResp, error)
	AcceptCall(ctx context.Context, req *AcceptCallReq) (*AcceptCallResp, error)
	RejectCall(ctx context.Context, req *RejectCallReq) (*RejectCallResp, error)
	CancelCall(ctx context.Context, req *CancelCallReq) (*CancelCallResp, error)
	GetActiveIncomingCall(ctx context.Context, req *GetActiveIncomingCallReq) (*GetActiveIncomingCallResp, error)
	SetMeetingInvitees(ctx context.Context, req *SetMeetingInviteesReq) (*SetMeetingInviteesResp, error)
	GetMeetingInviteesBatch(ctx context.Context, req *GetMeetingInviteesBatchReq) (*GetMeetingInviteesBatchResp, error)
	HandleLiveKitWebhookEvent(ctx context.Context, req *LiveKitWebhookEventReq) (*LiveKitWebhookEventResp, error)
}

type CallSignalingServiceClient interface {
	StartCall(ctx context.Context, req *StartCallReq, opts ...grpc.CallOption) (*StartCallResp, error)
	AcceptCall(ctx context.Context, req *AcceptCallReq, opts ...grpc.CallOption) (*AcceptCallResp, error)
	RejectCall(ctx context.Context, req *RejectCallReq, opts ...grpc.CallOption) (*RejectCallResp, error)
	CancelCall(ctx context.Context, req *CancelCallReq, opts ...grpc.CallOption) (*CancelCallResp, error)
	GetActiveIncomingCall(ctx context.Context, req *GetActiveIncomingCallReq, opts ...grpc.CallOption) (*GetActiveIncomingCallResp, error)
	SetMeetingInvitees(ctx context.Context, req *SetMeetingInviteesReq, opts ...grpc.CallOption) (*SetMeetingInviteesResp, error)
	GetMeetingInviteesBatch(ctx context.Context, req *GetMeetingInviteesBatchReq, opts ...grpc.CallOption) (*GetMeetingInviteesBatchResp, error)
	HandleLiveKitWebhookEvent(ctx context.Context, req *LiveKitWebhookEventReq, opts ...grpc.CallOption) (*LiveKitWebhookEventResp, error)
}

type clientImpl struct {
	cc grpc.ClientConnInterface
}

func NewCallSignalingServiceClient(cc grpc.ClientConnInterface) CallSignalingServiceClient {
	return &clientImpl{cc: cc}
}

func invokeJSON[Req any, Resp any](ctx context.Context, cc grpc.ClientConnInterface, method string, req *Req, opts ...grpc.CallOption) (*Resp, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	out := new(wrapperspb.BytesValue)
	if err := cc.Invoke(ctx, method, wrapperspb.Bytes(raw), out, opts...); err != nil {
		return nil, err
	}
	resp := new(Resp)
	if len(out.Value) > 0 {
		if err := json.Unmarshal(out.Value, resp); err != nil {
			return nil, err
		}
	}
	return resp, nil
}

func makeHandler[Req any, Resp any](fullMethod string, fn func(CallSignalingServiceServer, context.Context, *Req) (*Resp, error)) func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		in := new(wrapperspb.BytesValue)
		if err := dec(in); err != nil {
			return nil, err
		}
		handler := func(ctx context.Context, raw any) (any, error) {
			var req Req
			bv := raw.(*wrapperspb.BytesValue)
			if len(bv.Value) > 0 {
				if err := json.Unmarshal(bv.Value, &req); err != nil {
					return nil, err
				}
			}
			resp, err := fn(srv.(CallSignalingServiceServer), ctx, &req)
			if err != nil {
				return nil, err
			}
			encoded, err := json.Marshal(resp)
			if err != nil {
				return nil, err
			}
			return wrapperspb.Bytes(encoded), nil
		}
		if interceptor == nil {
			return handler(ctx, in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: fullMethod}
		return interceptor(ctx, in, info, handler)
	}
}

func (c *clientImpl) StartCall(ctx context.Context, req *StartCallReq, opts ...grpc.CallOption) (*StartCallResp, error) {
	return invokeJSON[StartCallReq, StartCallResp](ctx, c.cc, methodStartCall, req, opts...)
}
func (c *clientImpl) AcceptCall(ctx context.Context, req *AcceptCallReq, opts ...grpc.CallOption) (*AcceptCallResp, error) {
	return invokeJSON[AcceptCallReq, AcceptCallResp](ctx, c.cc, methodAcceptCall, req, opts...)
}
func (c *clientImpl) RejectCall(ctx context.Context, req *RejectCallReq, opts ...grpc.CallOption) (*RejectCallResp, error) {
	return invokeJSON[RejectCallReq, RejectCallResp](ctx, c.cc, methodRejectCall, req, opts...)
}
func (c *clientImpl) CancelCall(ctx context.Context, req *CancelCallReq, opts ...grpc.CallOption) (*CancelCallResp, error) {
	return invokeJSON[CancelCallReq, CancelCallResp](ctx, c.cc, methodCancelCall, req, opts...)
}
func (c *clientImpl) GetActiveIncomingCall(ctx context.Context, req *GetActiveIncomingCallReq, opts ...grpc.CallOption) (*GetActiveIncomingCallResp, error) {
	return invokeJSON[GetActiveIncomingCallReq, GetActiveIncomingCallResp](ctx, c.cc, methodGetActiveIncomingCall, req, opts...)
}
func (c *clientImpl) SetMeetingInvitees(ctx context.Context, req *SetMeetingInviteesReq, opts ...grpc.CallOption) (*SetMeetingInviteesResp, error) {
	return invokeJSON[SetMeetingInviteesReq, SetMeetingInviteesResp](ctx, c.cc, methodSetMeetingInvitees, req, opts...)
}
func (c *clientImpl) GetMeetingInviteesBatch(ctx context.Context, req *GetMeetingInviteesBatchReq, opts ...grpc.CallOption) (*GetMeetingInviteesBatchResp, error) {
	return invokeJSON[GetMeetingInviteesBatchReq, GetMeetingInviteesBatchResp](ctx, c.cc, methodGetMeetingInviteesBatch, req, opts...)
}
func (c *clientImpl) HandleLiveKitWebhookEvent(ctx context.Context, req *LiveKitWebhookEventReq, opts ...grpc.CallOption) (*LiveKitWebhookEventResp, error) {
	return invokeJSON[LiveKitWebhookEventReq, LiveKitWebhookEventResp](ctx, c.cc, methodHandleLiveKitWebhookEvent, req, opts...)
}

func RegisterCallSignalingServiceServer(s grpc.ServiceRegistrar, srv CallSignalingServiceServer) {
	s.RegisterService(&serviceDesc, srv)
}

var serviceDesc = grpc.ServiceDesc{
	ServiceName: serviceName,
	HandlerType: (*CallSignalingServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "StartCall", Handler: makeHandler(methodStartCall, CallSignalingServiceServer.StartCall)},
		{MethodName: "AcceptCall", Handler: makeHandler(methodAcceptCall, CallSignalingServiceServer.AcceptCall)},
		{MethodName: "RejectCall", Handler: makeHandler(methodRejectCall, CallSignalingServiceServer.RejectCall)},
		{MethodName: "CancelCall", Handler: makeHandler(methodCancelCall, CallSignalingServiceServer.CancelCall)},
		{MethodName: "GetActiveIncomingCall", Handler: makeHandler(methodGetActiveIncomingCall, CallSignalingServiceServer.GetActiveIncomingCall)},
		{MethodName: "SetMeetingInvitees", Handler: makeHandler(methodSetMeetingInvitees, CallSignalingServiceServer.SetMeetingInvitees)},
		{MethodName: "GetMeetingInviteesBatch", Handler: makeHandler(methodGetMeetingInviteesBatch, CallSignalingServiceServer.GetMeetingInviteesBatch)},
		{MethodName: "HandleLiveKitWebhookEvent", Handler: makeHandler(methodHandleLiveKitWebhookEvent, CallSignalingServiceServer.HandleLiveKitWebhookEvent)},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "openmeeting/meeting/call_signaling.proto",
}
