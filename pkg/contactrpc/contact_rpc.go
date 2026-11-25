package contactrpc

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	serviceName                 = "openmeeting.user.ContactDirectoryService"
	methodGetUserProfile        = "/" + serviceName + "/GetUserProfile"
	methodUpdateUserProfile     = "/" + serviceName + "/UpdateUserProfile"
	methodSearchDirectory       = "/" + serviceName + "/SearchDirectory"
	methodGetOrgTree            = "/" + serviceName + "/GetOrgTree"
	methodGetDeptMembers        = "/" + serviceName + "/GetDeptMembers"
	methodSyncContactSnapshot   = "/" + serviceName + "/SyncContactSnapshot"
	methodUpsertContactRelation = "/" + serviceName + "/UpsertContactRelation"
	methodRemoveContactRelation = "/" + serviceName + "/RemoveContactRelation"
	methodManageContactGroup    = "/" + serviceName + "/ManageContactGroup"
	methodSendContactRequest    = "/" + serviceName + "/SendContactRequest"
	methodHandleContactRequest  = "/" + serviceName + "/HandleContactRequest"
	methodListContactRequests   = "/" + serviceName + "/ListContactRequests"
	methodCheckBlocked          = "/" + serviceName + "/CheckBlocked"
)

type UserProfileDTO struct {
	UserID          string   `json:"userID"`
	Account         string   `json:"account"`
	Nickname        string   `json:"nickname"`
	EmployeeNo      string   `json:"employeeNo,omitempty"`
	Email           string   `json:"email,omitempty"`
	Phone           string   `json:"phone,omitempty"`
	JobTitle        string   `json:"jobTitle,omitempty"`
	Signature       string   `json:"signature,omitempty"`
	AvatarURL       string   `json:"avatarURL,omitempty"`
	PrimaryDeptIDs  []string `json:"primaryDeptIDs,omitempty"`
	DepartmentNames []string `json:"departmentNames,omitempty"`
	InOrgDirectory  bool     `json:"inOrgDirectory"`
	Status          int32    `json:"status"`
	UpdatedAtMs     int64    `json:"updatedAtMs"`
}

type OrgDepartmentDTO struct {
	DeptID           string `json:"deptID"`
	DeptCode         string `json:"deptCode,omitempty"`
	ParentID         string `json:"parentID"`
	Name             string `json:"name"`
	AncestorPath     string `json:"ancestorPath"`
	SortOrder        int32  `json:"sortOrder"`
	MemberVisibility string `json:"memberVisibility,omitempty"`
	Version          int64  `json:"version"`
}

type OrgEmployeeRecordDTO struct {
	RecordID      string   `json:"recordID"`
	OrgCode       string   `json:"orgCode,omitempty"`
	EmployeeNo    string   `json:"employeeNo"`
	JobTitle      string   `json:"jobTitle,omitempty"`
	DeptNames     []string `json:"deptNames,omitempty"`
	Status        int32    `json:"status"` // 1=active, 2=removed
	JoinedAtMs    int64    `json:"joinedAtMs"`
	RemovedAtMs   int64    `json:"removedAtMs,omitempty"`
	RemovedReason string   `json:"removedReason,omitempty"`
}

type DeptMemberDTO struct {
	DeptID    string         `json:"deptID"`
	IsLeader  bool           `json:"isLeader"`
	SortOrder int32          `json:"sortOrder"`
	Profile   UserProfileDTO `json:"profile"`
}

type ContactRelationDTO struct {
	OwnerUserID  string          `json:"ownerUserID"`
	TargetUserID string          `json:"targetUserID"`
	Remark       string          `json:"remark"`
	IsFavorite   bool            `json:"isFavorite"`
	IsBlocked    bool            `json:"isBlocked"`
	GroupIDs     []string        `json:"groupIDs"`
	SourceType   int32           `json:"sourceType"`
	CreatedAtMs  int64           `json:"createdAtMs"`
	UpdatedAtMs  int64           `json:"updatedAtMs"`
	Profile      *UserProfileDTO `json:"profile,omitempty"`
}

type ContactGroupDTO struct {
	GroupID     string `json:"groupID"`
	Name        string `json:"name"`
	SortOrder   int32  `json:"sortOrder"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
}

type ContactRequestDTO struct {
	RequestID   string          `json:"requestID"`
	FromUserID  string          `json:"fromUserID"`
	ToUserID    string          `json:"toUserID"`
	Greeting    string          `json:"greeting"`
	Status      int32           `json:"status"` // 0=pending, 1=accepted, 2=rejected
	CreatedAtMs int64           `json:"createdAtMs"`
	HandledAtMs int64           `json:"handledAtMs"`
	FromProfile *UserProfileDTO `json:"fromProfile,omitempty"`
	ToProfile   *UserProfileDTO `json:"toProfile,omitempty"`
}

type GetUserProfileReq struct {
	OperatorUserID string `json:"operatorUserID"`
	TargetUserID   string `json:"targetUserID"`
}

type GetUserProfileResp struct {
	Profile    UserProfileDTO         `json:"profile"`
	Relation   *ContactRelationDTO    `json:"relation,omitempty"`
	OrgHistory []OrgEmployeeRecordDTO `json:"orgHistory,omitempty"`
}

type UpdateUserProfileReq struct {
	UserID    string  `json:"userID"`
	Nickname  *string `json:"nickname,omitempty"`
	Signature *string `json:"signature,omitempty"`
	Email     *string `json:"email,omitempty"`
	Phone     *string `json:"phone,omitempty"`
}

type UpdateUserProfileResp struct {
	Profile UserProfileDTO `json:"profile"`
}

type SearchDirectoryReq struct {
	OperatorUserID string `json:"operatorUserID"`
	Keyword        string `json:"keyword"`
	DeptID         string `json:"deptID,omitempty"`
	Scope          string `json:"scope,omitempty"` // "all" | "org" | "contacts" | "exact"
	Offset         int64  `json:"offset,omitempty"`
	Limit          int64  `json:"limit,omitempty"`
}

type SearchDirectoryResp struct {
	Total int64            `json:"total"`
	Users []UserProfileDTO `json:"users"`
}

type GetOrgTreeReq struct {
	OperatorUserID string `json:"operatorUserID"`
	ClientVersion  int64  `json:"clientVersion,omitempty"`
}

type GetOrgTreeResp struct {
	TreeVersion      int64              `json:"treeVersion"`
	NotModified      bool               `json:"notModified"`
	MemberVisibility string             `json:"memberVisibility,omitempty"`
	Departments      []OrgDepartmentDTO `json:"departments"`
}

type GetDeptMembersReq struct {
	OperatorUserID string `json:"operatorUserID"`
	DeptID         string `json:"deptID"`
	Offset         int64  `json:"offset,omitempty"`
	Limit          int64  `json:"limit,omitempty"`
}

type GetDeptMembersResp struct {
	DeptID     string          `json:"deptID"`
	Total      int64           `json:"total"`
	Members    []DeptMemberDTO `json:"members"`
	Restricted bool            `json:"restricted,omitempty"`
}

type SyncContactSnapshotReq struct {
	UserID         string `json:"userID"`
	UpdatedAfterMs int64  `json:"updatedAfterMs,omitempty"`
}

type SyncContactSnapshotResp struct {
	ServerTimeMs int64                `json:"serverTimeMs"`
	Contacts     []ContactRelationDTO `json:"contacts"`
	Groups       []ContactGroupDTO    `json:"groups"`
	PendingCount int64                `json:"pendingCount"`
}

type UpsertContactRelationReq struct {
	OwnerUserID  string   `json:"ownerUserID"`
	TargetUserID string   `json:"targetUserID"`
	Remark       *string  `json:"remark,omitempty"`
	IsFavorite   *bool    `json:"isFavorite,omitempty"`
	IsBlocked    *bool    `json:"isBlocked,omitempty"`
	GroupIDs     []string `json:"groupIDs,omitempty"`
	HasGroupIDs  bool     `json:"hasGroupIDs,omitempty"`
}

type UpsertContactRelationResp struct {
	Relation ContactRelationDTO `json:"relation"`
}

type RemoveContactRelationReq struct {
	OwnerUserID  string `json:"ownerUserID"`
	TargetUserID string `json:"targetUserID"`
}

type RemoveContactRelationResp struct {
	Removed bool `json:"removed"`
}

type ManageContactGroupReq struct {
	OwnerUserID string `json:"ownerUserID"`
	Action      string `json:"action"` // "create" | "rename" | "reorder" | "delete"
	GroupID     string `json:"groupID,omitempty"`
	Name        string `json:"name,omitempty"`
	SortOrder   int32  `json:"sortOrder,omitempty"`
}

type ManageContactGroupResp struct {
	Group  *ContactGroupDTO  `json:"group,omitempty"`
	Groups []ContactGroupDTO `json:"groups"`
}

type SendContactRequestReq struct {
	FromUserID string `json:"fromUserID"`
	ToUserID   string `json:"toUserID"`
	Greeting   string `json:"greeting,omitempty"`
}

type SendContactRequestResp struct {
	Request      *ContactRequestDTO  `json:"request,omitempty"`
	AutoAccepted bool                `json:"autoAccepted"`
	Relation     *ContactRelationDTO `json:"relation,omitempty"`
}

type HandleContactRequestReq struct {
	OperatorUserID string `json:"operatorUserID"`
	RequestID      string `json:"requestID"`
	Accept         bool   `json:"accept"`
}

type HandleContactRequestResp struct {
	Request  ContactRequestDTO   `json:"request"`
	Relation *ContactRelationDTO `json:"relation,omitempty"`
}

type ListContactRequestsReq struct {
	UserID string `json:"userID"`
	Limit  int64  `json:"limit,omitempty"`
}

type ListContactRequestsResp struct {
	Requests     []ContactRequestDTO `json:"requests"`
	PendingCount int64               `json:"pendingCount"`
}

type CheckBlockedReq struct {
	OwnerUserID  string `json:"ownerUserID"`
	TargetUserID string `json:"targetUserID"`
}

type CheckBlockedResp struct {
	Blocked bool `json:"blocked"`
}

type ContactDirectoryServiceServer interface {
	GetUserProfile(ctx context.Context, req *GetUserProfileReq) (*GetUserProfileResp, error)
	UpdateUserProfile(ctx context.Context, req *UpdateUserProfileReq) (*UpdateUserProfileResp, error)
	SearchDirectory(ctx context.Context, req *SearchDirectoryReq) (*SearchDirectoryResp, error)
	GetOrgTree(ctx context.Context, req *GetOrgTreeReq) (*GetOrgTreeResp, error)
	GetDeptMembers(ctx context.Context, req *GetDeptMembersReq) (*GetDeptMembersResp, error)
	SyncContactSnapshot(ctx context.Context, req *SyncContactSnapshotReq) (*SyncContactSnapshotResp, error)
	UpsertContactRelation(ctx context.Context, req *UpsertContactRelationReq) (*UpsertContactRelationResp, error)
	RemoveContactRelation(ctx context.Context, req *RemoveContactRelationReq) (*RemoveContactRelationResp, error)
	ManageContactGroup(ctx context.Context, req *ManageContactGroupReq) (*ManageContactGroupResp, error)
	SendContactRequest(ctx context.Context, req *SendContactRequestReq) (*SendContactRequestResp, error)
	HandleContactRequest(ctx context.Context, req *HandleContactRequestReq) (*HandleContactRequestResp, error)
	ListContactRequests(ctx context.Context, req *ListContactRequestsReq) (*ListContactRequestsResp, error)
	CheckBlocked(ctx context.Context, req *CheckBlockedReq) (*CheckBlockedResp, error)
}

type ContactDirectoryServiceClient interface {
	GetUserProfile(ctx context.Context, req *GetUserProfileReq, opts ...grpc.CallOption) (*GetUserProfileResp, error)
	UpdateUserProfile(ctx context.Context, req *UpdateUserProfileReq, opts ...grpc.CallOption) (*UpdateUserProfileResp, error)
	SearchDirectory(ctx context.Context, req *SearchDirectoryReq, opts ...grpc.CallOption) (*SearchDirectoryResp, error)
	GetOrgTree(ctx context.Context, req *GetOrgTreeReq, opts ...grpc.CallOption) (*GetOrgTreeResp, error)
	GetDeptMembers(ctx context.Context, req *GetDeptMembersReq, opts ...grpc.CallOption) (*GetDeptMembersResp, error)
	SyncContactSnapshot(ctx context.Context, req *SyncContactSnapshotReq, opts ...grpc.CallOption) (*SyncContactSnapshotResp, error)
	UpsertContactRelation(ctx context.Context, req *UpsertContactRelationReq, opts ...grpc.CallOption) (*UpsertContactRelationResp, error)
	RemoveContactRelation(ctx context.Context, req *RemoveContactRelationReq, opts ...grpc.CallOption) (*RemoveContactRelationResp, error)
	ManageContactGroup(ctx context.Context, req *ManageContactGroupReq, opts ...grpc.CallOption) (*ManageContactGroupResp, error)
	SendContactRequest(ctx context.Context, req *SendContactRequestReq, opts ...grpc.CallOption) (*SendContactRequestResp, error)
	HandleContactRequest(ctx context.Context, req *HandleContactRequestReq, opts ...grpc.CallOption) (*HandleContactRequestResp, error)
	ListContactRequests(ctx context.Context, req *ListContactRequestsReq, opts ...grpc.CallOption) (*ListContactRequestsResp, error)
	CheckBlocked(ctx context.Context, req *CheckBlockedReq, opts ...grpc.CallOption) (*CheckBlockedResp, error)
}

type clientImpl struct {
	cc grpc.ClientConnInterface
}

func NewContactDirectoryServiceClient(cc grpc.ClientConnInterface) ContactDirectoryServiceClient {
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

func makeHandler[Req any, Resp any](fullMethod string, fn func(ContactDirectoryServiceServer, context.Context, *Req) (*Resp, error)) func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
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
			resp, err := fn(srv.(ContactDirectoryServiceServer), ctx, &req)
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

func (c *clientImpl) GetUserProfile(ctx context.Context, req *GetUserProfileReq, opts ...grpc.CallOption) (*GetUserProfileResp, error) {
	return invokeJSON[GetUserProfileReq, GetUserProfileResp](ctx, c.cc, methodGetUserProfile, req, opts...)
}
func (c *clientImpl) UpdateUserProfile(ctx context.Context, req *UpdateUserProfileReq, opts ...grpc.CallOption) (*UpdateUserProfileResp, error) {
	return invokeJSON[UpdateUserProfileReq, UpdateUserProfileResp](ctx, c.cc, methodUpdateUserProfile, req, opts...)
}
func (c *clientImpl) SearchDirectory(ctx context.Context, req *SearchDirectoryReq, opts ...grpc.CallOption) (*SearchDirectoryResp, error) {
	return invokeJSON[SearchDirectoryReq, SearchDirectoryResp](ctx, c.cc, methodSearchDirectory, req, opts...)
}
func (c *clientImpl) GetOrgTree(ctx context.Context, req *GetOrgTreeReq, opts ...grpc.CallOption) (*GetOrgTreeResp, error) {
	return invokeJSON[GetOrgTreeReq, GetOrgTreeResp](ctx, c.cc, methodGetOrgTree, req, opts...)
}
func (c *clientImpl) GetDeptMembers(ctx context.Context, req *GetDeptMembersReq, opts ...grpc.CallOption) (*GetDeptMembersResp, error) {
	return invokeJSON[GetDeptMembersReq, GetDeptMembersResp](ctx, c.cc, methodGetDeptMembers, req, opts...)
}
func (c *clientImpl) SyncContactSnapshot(ctx context.Context, req *SyncContactSnapshotReq, opts ...grpc.CallOption) (*SyncContactSnapshotResp, error) {
	return invokeJSON[SyncContactSnapshotReq, SyncContactSnapshotResp](ctx, c.cc, methodSyncContactSnapshot, req, opts...)
}
func (c *clientImpl) UpsertContactRelation(ctx context.Context, req *UpsertContactRelationReq, opts ...grpc.CallOption) (*UpsertContactRelationResp, error) {
	return invokeJSON[UpsertContactRelationReq, UpsertContactRelationResp](ctx, c.cc, methodUpsertContactRelation, req, opts...)
}
func (c *clientImpl) RemoveContactRelation(ctx context.Context, req *RemoveContactRelationReq, opts ...grpc.CallOption) (*RemoveContactRelationResp, error) {
	return invokeJSON[RemoveContactRelationReq, RemoveContactRelationResp](ctx, c.cc, methodRemoveContactRelation, req, opts...)
}
func (c *clientImpl) ManageContactGroup(ctx context.Context, req *ManageContactGroupReq, opts ...grpc.CallOption) (*ManageContactGroupResp, error) {
	return invokeJSON[ManageContactGroupReq, ManageContactGroupResp](ctx, c.cc, methodManageContactGroup, req, opts...)
}
func (c *clientImpl) SendContactRequest(ctx context.Context, req *SendContactRequestReq, opts ...grpc.CallOption) (*SendContactRequestResp, error) {
	return invokeJSON[SendContactRequestReq, SendContactRequestResp](ctx, c.cc, methodSendContactRequest, req, opts...)
}
func (c *clientImpl) HandleContactRequest(ctx context.Context, req *HandleContactRequestReq, opts ...grpc.CallOption) (*HandleContactRequestResp, error) {
	return invokeJSON[HandleContactRequestReq, HandleContactRequestResp](ctx, c.cc, methodHandleContactRequest, req, opts...)
}
func (c *clientImpl) ListContactRequests(ctx context.Context, req *ListContactRequestsReq, opts ...grpc.CallOption) (*ListContactRequestsResp, error) {
	return invokeJSON[ListContactRequestsReq, ListContactRequestsResp](ctx, c.cc, methodListContactRequests, req, opts...)
}
func (c *clientImpl) CheckBlocked(ctx context.Context, req *CheckBlockedReq, opts ...grpc.CallOption) (*CheckBlockedResp, error) {
	return invokeJSON[CheckBlockedReq, CheckBlockedResp](ctx, c.cc, methodCheckBlocked, req, opts...)
}

func RegisterContactDirectoryServiceServer(s grpc.ServiceRegistrar, srv ContactDirectoryServiceServer) {
	s.RegisterService(&serviceDesc, srv)
}

var serviceDesc = grpc.ServiceDesc{
	ServiceName: serviceName,
	HandlerType: (*ContactDirectoryServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{MethodName: "GetUserProfile", Handler: makeHandler(methodGetUserProfile, ContactDirectoryServiceServer.GetUserProfile)},
		{MethodName: "UpdateUserProfile", Handler: makeHandler(methodUpdateUserProfile, ContactDirectoryServiceServer.UpdateUserProfile)},
		{MethodName: "SearchDirectory", Handler: makeHandler(methodSearchDirectory, ContactDirectoryServiceServer.SearchDirectory)},
		{MethodName: "GetOrgTree", Handler: makeHandler(methodGetOrgTree, ContactDirectoryServiceServer.GetOrgTree)},
		{MethodName: "GetDeptMembers", Handler: makeHandler(methodGetDeptMembers, ContactDirectoryServiceServer.GetDeptMembers)},
		{MethodName: "SyncContactSnapshot", Handler: makeHandler(methodSyncContactSnapshot, ContactDirectoryServiceServer.SyncContactSnapshot)},
		{MethodName: "UpsertContactRelation", Handler: makeHandler(methodUpsertContactRelation, ContactDirectoryServiceServer.UpsertContactRelation)},
		{MethodName: "RemoveContactRelation", Handler: makeHandler(methodRemoveContactRelation, ContactDirectoryServiceServer.RemoveContactRelation)},
		{MethodName: "ManageContactGroup", Handler: makeHandler(methodManageContactGroup, ContactDirectoryServiceServer.ManageContactGroup)},
		{MethodName: "SendContactRequest", Handler: makeHandler(methodSendContactRequest, ContactDirectoryServiceServer.SendContactRequest)},
		{MethodName: "HandleContactRequest", Handler: makeHandler(methodHandleContactRequest, ContactDirectoryServiceServer.HandleContactRequest)},
		{MethodName: "ListContactRequests", Handler: makeHandler(methodListContactRequests, ContactDirectoryServiceServer.ListContactRequests)},
		{MethodName: "CheckBlocked", Handler: makeHandler(methodCheckBlocked, ContactDirectoryServiceServer.CheckBlocked)},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "openmeeting/user/contact_directory.proto",
}
