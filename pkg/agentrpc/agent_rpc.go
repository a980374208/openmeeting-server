package agentrpc

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	serviceName             = "openmeeting.meeting.AgentTokenService"
	methodRequestAgentToken = "/" + serviceName + "/RequestAgentToken"
	methodGetAgentStatus    = "/" + serviceName + "/GetAgentStatus"
)

type LiveKitInfo struct {
	URL   string `json:"url"`
	Token string `json:"token"`
}

type ExistingAgentInfo struct {
	Identity       string `json:"identity"`
	Name           string `json:"name"`
	OwnerUserID    string `json:"ownerUserID"`
	Role           string `json:"role"`
	State          string `json:"state"`
	TargetLanguage string `json:"targetLanguage"`
}

type RequestAgentTokenReq struct {
	MeetingID      string `json:"meetingID"`
	UserID         string `json:"userID"`
	Role           string `json:"role"`
	DisplayName    string `json:"displayName"`
	SourceLanguage string `json:"sourceLanguage"`
	TargetLanguage string `json:"targetLanguage"`
	ForceTakeover  bool   `json:"forceTakeover"`
}

type RequestAgentTokenResp struct {
	MeetingID             string             `json:"meetingID,omitempty"`
	AgentIdentity         string             `json:"agentIdentity,omitempty"`
	LiveKit               *LiveKitInfo       `json:"liveKit,omitempty"`
	ExpiresAtSeconds      int64              `json:"expiresAtSeconds,omitempty"`
	ReplacedAgentIdentity string             `json:"replacedAgentIdentity,omitempty"`
	Conflict              bool               `json:"conflict,omitempty"`
	ExistingAgent         *ExistingAgentInfo `json:"existingAgent,omitempty"`
}

type GetAgentStatusReq struct {
	MeetingID string `json:"meetingID"`
	UserID    string `json:"userID"`
}

type GetAgentStatusResp struct {
	MeetingID      string              `json:"meetingID"`
	HasOnlineAgent bool                `json:"hasOnlineAgent"`
	Agents         []ExistingAgentInfo `json:"agents"`
}

type AgentTokenServiceServer interface {
	RequestAgentToken(ctx context.Context, req *RequestAgentTokenReq) (*RequestAgentTokenResp, error)
	GetAgentStatus(ctx context.Context, req *GetAgentStatusReq) (*GetAgentStatusResp, error)
}

type AgentTokenServiceClient interface {
	RequestAgentToken(ctx context.Context, req *RequestAgentTokenReq, opts ...grpc.CallOption) (*RequestAgentTokenResp, error)
	GetAgentStatus(ctx context.Context, req *GetAgentStatusReq, opts ...grpc.CallOption) (*GetAgentStatusResp, error)
}

type agentTokenServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewAgentTokenServiceClient(cc grpc.ClientConnInterface) AgentTokenServiceClient {
	return &agentTokenServiceClient{cc: cc}
}

func (c *agentTokenServiceClient) RequestAgentToken(ctx context.Context, req *RequestAgentTokenReq, opts ...grpc.CallOption) (*RequestAgentTokenResp, error) {
	in, err := toStruct(req)
	if err != nil {
		return nil, err
	}
	out := new(structpb.Struct)
	if err := c.cc.Invoke(ctx, methodRequestAgentToken, in, out, opts...); err != nil {
		return nil, err
	}
	resp := new(RequestAgentTokenResp)
	if err := fromStruct(out, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *agentTokenServiceClient) GetAgentStatus(ctx context.Context, req *GetAgentStatusReq, opts ...grpc.CallOption) (*GetAgentStatusResp, error) {
	in, err := toStruct(req)
	if err != nil {
		return nil, err
	}
	out := new(structpb.Struct)
	if err := c.cc.Invoke(ctx, methodGetAgentStatus, in, out, opts...); err != nil {
		return nil, err
	}
	resp := new(GetAgentStatusResp)
	if err := fromStruct(out, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func RegisterAgentTokenServiceServer(s grpc.ServiceRegistrar, srv AgentTokenServiceServer) {
	s.RegisterService(&agentTokenServiceDesc, srv)
}

func requestAgentTokenHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(structpb.Struct)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, raw any) (any, error) {
		var req RequestAgentTokenReq
		if err := fromStruct(raw.(*structpb.Struct), &req); err != nil {
			return nil, err
		}
		resp, err := srv.(AgentTokenServiceServer).RequestAgentToken(ctx, &req)
		if err != nil {
			return nil, err
		}
		return toStruct(resp)
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: methodRequestAgentToken,
	}
	return interceptor(ctx, in, info, handler)
}

func getAgentStatusHandler(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
	in := new(structpb.Struct)
	if err := dec(in); err != nil {
		return nil, err
	}
	handler := func(ctx context.Context, raw any) (any, error) {
		var req GetAgentStatusReq
		if err := fromStruct(raw.(*structpb.Struct), &req); err != nil {
			return nil, err
		}
		resp, err := srv.(AgentTokenServiceServer).GetAgentStatus(ctx, &req)
		if err != nil {
			return nil, err
		}
		return toStruct(resp)
	}
	if interceptor == nil {
		return handler(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: methodGetAgentStatus,
	}
	return interceptor(ctx, in, info, handler)
}

var agentTokenServiceDesc = grpc.ServiceDesc{
	ServiceName: serviceName,
	HandlerType: (*AgentTokenServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "RequestAgentToken",
			Handler:    requestAgentTokenHandler,
		},
		{
			MethodName: "GetAgentStatus",
			Handler:    getAgentStatusHandler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "openmeeting/meeting/agent_token.proto",
}

func toStruct(v any) (*structpb.Struct, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return structpb.NewStruct(m)
}

func fromStruct(s *structpb.Struct, dst any) error {
	if s == nil {
		return nil
	}
	raw, err := json.Marshal(s.AsMap())
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
