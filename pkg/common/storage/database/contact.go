package database

import (
	"context"

	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
)

type Contact interface {
	// Organization tree
	ListDepartments(ctx context.Context) ([]*model.OrgDepartment, int64, error)
	GetDepartment(ctx context.Context, deptID string) (*model.OrgDepartment, error)
	UpsertDepartment(ctx context.Context, dept *model.OrgDepartment) error
	EnsureDepartmentPath(ctx context.Context, slashPath string) ([]*model.OrgDepartment, error)
	SetUserDepartments(ctx context.Context, userID string, deptIDs []string, isLeader bool) error
	ListDepartmentMembers(ctx context.Context, deptID string, offset, limit int64) ([]*model.UserDepartmentMember, int64, error)
	ListAllDepartmentMembers(ctx context.Context) ([]*model.UserDepartmentMember, error)
	ListUserDepartments(ctx context.Context, userID string) ([]*model.UserDepartmentMember, error)
	ReplaceOrgSnapshot(ctx context.Context, depts []*model.OrgDepartment, members []*model.UserDepartmentMember, minVersion int64) (int64, error)
	BumpOrgTreeVersion(ctx context.Context, minVersion int64) (int64, error)
	GetOrgMemberVisibility(ctx context.Context) (string, error)
	SetOrgMemberVisibility(ctx context.Context, visibility string) error
	EnsureOrgCode(ctx context.Context) (string, error)
	AllocateDeptCode(ctx context.Context) (string, error)
	AllocateEmployeeNo(ctx context.Context) (string, error)
	CreateEmployeeRecord(ctx context.Context, rec *model.OrgEmployeeRecord) error
	UpdateActiveEmployeeRecord(ctx context.Context, userID, nickname, jobTitle string, deptNames []string) error
	MarkEmployeeRecordRemoved(ctx context.Context, userID string, removedAt int64, reason, removedByUserID, removedByAccount string, lastDeptNames []string) (int64, error)
	ListUserEmployeeRecords(ctx context.Context, userID string) ([]*model.OrgEmployeeRecord, error)
	ListAllEmployeeRecords(ctx context.Context) ([]*model.OrgEmployeeRecord, error)
	AreInSameOrgDirectory(ctx context.Context, userA, userB string) (bool, error)

	// Personal contacts & groups
	ListRelations(ctx context.Context, ownerUserID string, updatedAfterMs int64) ([]*model.ContactRelation, error)
	GetRelation(ctx context.Context, ownerUserID, targetUserID string) (*model.ContactRelation, error)
	UpsertRelation(ctx context.Context, rel *model.ContactRelation) error
	RemoveRelation(ctx context.Context, ownerUserID, targetUserID string) error
	IsBlocked(ctx context.Context, ownerUserID, targetUserID string) (bool, error)

	ListGroups(ctx context.Context, ownerUserID string) ([]*model.ContactGroup, error)
	UpsertGroup(ctx context.Context, group *model.ContactGroup) error
	DeleteGroup(ctx context.Context, ownerUserID, groupID string) error

	// Contact requests (for external / cross-boundary accounts)
	CreateRequest(ctx context.Context, req *model.ContactRequest) error
	GetRequest(ctx context.Context, requestID string) (*model.ContactRequest, error)
	FindPendingRequest(ctx context.Context, fromUserID, toUserID string) (*model.ContactRequest, error)
	UpdateRequestStatus(ctx context.Context, requestID string, expectedStatus, newStatus int32, handledAt int64) (bool, error)
	ListRequests(ctx context.Context, userID string, limit int64) ([]*model.ContactRequest, error)
}
