package user

import (
	"context"
	"testing"

	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/openmeeting-server/pkg/contactrpc"
	"github.com/openimsdk/tools/errs"
)

type visibilityStubContactDB struct {
	depts            []*model.OrgDepartment
	members          []*model.UserDepartmentMember
	empRecords       []*model.OrgEmployeeRecord
	version          int64
	memberVisibility string
}

func (s *visibilityStubContactDB) ListDepartments(_ context.Context) ([]*model.OrgDepartment, int64, error) {
	return s.depts, s.version, nil
}
func (s *visibilityStubContactDB) GetDepartment(_ context.Context, deptID string) (*model.OrgDepartment, error) {
	for _, d := range s.depts {
		if d.DeptID == deptID {
			return d, nil
		}
	}
	return nil, errs.ErrRecordNotFound
}
func (s *visibilityStubContactDB) UpsertDepartment(_ context.Context, _ *model.OrgDepartment) error {
	return nil
}
func (s *visibilityStubContactDB) EnsureDepartmentPath(_ context.Context, _ string) ([]*model.OrgDepartment, error) {
	return nil, nil
}
func (s *visibilityStubContactDB) SetUserDepartments(_ context.Context, _ string, _ []string, _ bool) error {
	return nil
}
func (s *visibilityStubContactDB) ListDepartmentMembers(_ context.Context, deptID string, _, _ int64) ([]*model.UserDepartmentMember, int64, error) {
	var out []*model.UserDepartmentMember
	for _, m := range s.members {
		if m.DeptID == deptID {
			out = append(out, m)
		}
	}
	return out, int64(len(out)), nil
}
func (s *visibilityStubContactDB) ListAllDepartmentMembers(_ context.Context) ([]*model.UserDepartmentMember, error) {
	return s.members, nil
}
func (s *visibilityStubContactDB) ListUserDepartments(_ context.Context, userID string) ([]*model.UserDepartmentMember, error) {
	var out []*model.UserDepartmentMember
	for _, m := range s.members {
		if m.UserID == userID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *visibilityStubContactDB) ReplaceOrgSnapshot(_ context.Context, _ []*model.OrgDepartment, _ []*model.UserDepartmentMember, minVersion int64) (int64, error) {
	return minVersion, nil
}
func (s *visibilityStubContactDB) BumpOrgTreeVersion(_ context.Context, minVersion int64) (int64, error) {
	return minVersion, nil
}
func (s *visibilityStubContactDB) GetOrgMemberVisibility(_ context.Context) (string, error) {
	return model.NormalizeOrgMemberVisibility(s.memberVisibility), nil
}
func (s *visibilityStubContactDB) SetOrgMemberVisibility(_ context.Context, visibility string) error {
	s.memberVisibility = model.NormalizeOrgMemberVisibility(visibility)
	return nil
}
func (s *visibilityStubContactDB) EnsureOrgCode(_ context.Context) (string, error)    { return "O00001", nil }
func (s *visibilityStubContactDB) AllocateDeptCode(_ context.Context) (string, error) { return "D00001", nil }
func (s *visibilityStubContactDB) AllocateEmployeeNo(_ context.Context) (string, error) {
	return "E00001", nil
}
func (s *visibilityStubContactDB) CreateEmployeeRecord(_ context.Context, rec *model.OrgEmployeeRecord) error {
	s.empRecords = append(s.empRecords, rec)
	return nil
}
func (s *visibilityStubContactDB) UpdateActiveEmployeeRecord(_ context.Context, _, _, _ string, _ []string) error {
	return nil
}
func (s *visibilityStubContactDB) MarkEmployeeRecordRemoved(_ context.Context, _ string, _ int64, _, _, _ string, _ []string) (int64, error) {
	return 0, nil
}
func (s *visibilityStubContactDB) ListUserEmployeeRecords(_ context.Context, userID string) ([]*model.OrgEmployeeRecord, error) {
	var out []*model.OrgEmployeeRecord
	for _, r := range s.empRecords {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (s *visibilityStubContactDB) ListAllEmployeeRecords(_ context.Context) ([]*model.OrgEmployeeRecord, error) {
	return s.empRecords, nil
}
func (s *visibilityStubContactDB) AreInSameOrgDirectory(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (s *visibilityStubContactDB) ListRelations(_ context.Context, _ string, _ int64) ([]*model.ContactRelation, error) {
	return nil, nil
}
func (s *visibilityStubContactDB) GetRelation(_ context.Context, _, _ string) (*model.ContactRelation, error) {
	return nil, errs.ErrRecordNotFound
}
func (s *visibilityStubContactDB) UpsertRelation(_ context.Context, _ *model.ContactRelation) error {
	return nil
}
func (s *visibilityStubContactDB) RemoveRelation(_ context.Context, _, _ string) error { return nil }
func (s *visibilityStubContactDB) IsBlocked(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (s *visibilityStubContactDB) ListGroups(_ context.Context, _ string) ([]*model.ContactGroup, error) {
	return nil, nil
}
func (s *visibilityStubContactDB) UpsertGroup(_ context.Context, _ *model.ContactGroup) error {
	return nil
}
func (s *visibilityStubContactDB) DeleteGroup(_ context.Context, _, _ string) error { return nil }
func (s *visibilityStubContactDB) CreateRequest(_ context.Context, _ *model.ContactRequest) error {
	return nil
}
func (s *visibilityStubContactDB) GetRequest(_ context.Context, _ string) (*model.ContactRequest, error) {
	return nil, errs.ErrRecordNotFound
}
func (s *visibilityStubContactDB) FindPendingRequest(_ context.Context, _, _ string) (*model.ContactRequest, error) {
	return nil, nil
}
func (s *visibilityStubContactDB) UpdateRequestStatus(_ context.Context, _ string, _, _ int32, _ int64) (bool, error) {
	return false, nil
}
func (s *visibilityStubContactDB) ListRequests(_ context.Context, _ string, _ int64) ([]*model.ContactRequest, error) {
	return nil, nil
}

func TestOrgTreeVisibilityOnlyForOrgMembers(t *testing.T) {
	db := &visibilityStubContactDB{
		version: 1001,
		depts: []*model.OrgDepartment{
			{DeptID: "dept_rd", ParentID: "", Name: "研发部", AncestorPath: "/", Version: 1001},
		},
		members: []*model.UserDepartmentMember{
			{DeptID: "dept_rd", UserID: "u-employee", IsLeader: true},
		},
	}
	srv := &userServer{contactDB: db}

	// 1. User belonging to the org receives the full tree.
	empResp, err := srv.GetOrgTree(context.Background(), &contactrpc.GetOrgTreeReq{
		OperatorUserID: "u-employee",
		ClientVersion:  0,
	})
	if err != nil {
		t.Fatalf("employee GetOrgTree err: %v", err)
	}
	if len(empResp.Departments) != 1 || empResp.NotModified || empResp.TreeVersion != 1001 {
		t.Fatalf("expected 1 dept for employee, got %+v", empResp)
	}

	// 2. External user NOT in any department receives empty departments (NotModified=false).
	extResp, err := srv.GetOrgTree(context.Background(), &contactrpc.GetOrgTreeReq{
		OperatorUserID: "u-external",
		ClientVersion:  1001,
	})
	if err != nil {
		t.Fatalf("external GetOrgTree err: %v", err)
	}
	if len(extResp.Departments) != 0 || extResp.NotModified {
		t.Fatalf("expected empty non-modified response for external user, got %+v", extResp)
	}

	// 3. External user calling GetDeptMembers receives empty list.
	extMemResp, err := srv.GetDeptMembers(context.Background(), &contactrpc.GetDeptMembersReq{
		OperatorUserID: "u-external",
		DeptID:         "dept_rd",
	})
	if err != nil {
		t.Fatalf("external GetDeptMembers err: %v", err)
	}
	if len(extMemResp.Members) != 0 || extMemResp.Total != 0 {
		t.Fatalf("expected empty dept members for external user, got %+v", extMemResp)
	}
}

type profileStubUserCtrl struct {
	users map[string]*model.User
}

func (s *profileStubUserCtrl) FindWithError(_ context.Context, ids []string) ([]*model.User, error) {
	return s.FindByUserIDs(context.Background(), ids)
}
func (s *profileStubUserCtrl) Create(_ context.Context, _ []*model.User) error          { return nil }
func (s *profileStubUserCtrl) GetByAccount(_ context.Context, _ string) (*model.User, error) {
	return nil, errs.ErrRecordNotFound
}
func (s *profileStubUserCtrl) StoreToken(_ context.Context, _, _ string) error { return nil }
func (s *profileStubUserCtrl) GetToken(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *profileStubUserCtrl) ClearUserToken(_ context.Context, _ string) error { return nil }
func (s *profileStubUserCtrl) GenerateUserID(_ context.Context) (string, error) { return "u1", nil }
func (s *profileStubUserCtrl) TakeByExactIdentifier(_ context.Context, _ string) (*model.User, error) {
	return nil, errs.ErrRecordNotFound
}
func (s *profileStubUserCtrl) FindByUserIDs(_ context.Context, ids []string) ([]*model.User, error) {
	var out []*model.User
	for _, id := range ids {
		if u, ok := s.users[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}
func (s *profileStubUserCtrl) UpdateProfile(_ context.Context, userID, _ string, update map[string]any) error {
	u, ok := s.users[userID]
	if !ok {
		return errs.ErrRecordNotFound
	}
	if v, ok := update["nickname"].(string); ok {
		u.Nickname = v
	}
	if v, ok := update["email"].(string); ok {
		u.Email = v
	}
	if v, ok := update["phone"].(string); ok {
		u.Phone = v
	}
	if v, ok := update["signature"].(string); ok {
		u.Signature = v
	}
	return nil
}
func (s *profileStubUserCtrl) UpsertByAccount(_ context.Context, _ *model.User) error { return nil }
func (s *profileStubUserCtrl) SearchByPrefix(_ context.Context, _, _ string, _ []string, _, _ int64) ([]*model.User, int64, error) {
	return nil, 0, nil
}
func (s *profileStubUserCtrl) ListAll(_ context.Context, _ string, _, _ int64) ([]*model.User, int64, error) {
	return nil, 0, nil
}

func TestOrgHistorySelfOnlyAndProfileSelfEdit(t *testing.T) {
	db := &visibilityStubContactDB{
		empRecords: []*model.OrgEmployeeRecord{
			{
				RecordID:      "rec_1",
				OrgCode:       "O00001",
				EmployeeNo:    "E00001",
				UserID:        "u-a",
				Status:        model.OrgEmployeeStatusRemoved,
				JoinedAt:      100,
				RemovedAt:     200,
				RemovedReason: "离职归档",
			},
		},
	}
	userCtrl := &profileStubUserCtrl{
		users: map[string]*model.User{
			"u-a": {UserID: "u-a", Account: "10000", Nickname: "Alice", EmployeeNo: "E00001", JobTitle: "工程师"},
			"u-b": {UserID: "u-b", Account: "10001", Nickname: "Bob"},
		},
	}
	srv := &userServer{contactDB: db, userStorageHandler: userCtrl}

	// 1. Self profile request returns OrgHistory with removal reason.
	selfResp, err := srv.GetUserProfile(context.Background(), &contactrpc.GetUserProfileReq{
		OperatorUserID: "u-a",
		TargetUserID:   "u-a",
	})
	if err != nil {
		t.Fatalf("self GetUserProfile failed: %v", err)
	}
	if len(selfResp.OrgHistory) != 1 || selfResp.OrgHistory[0].EmployeeNo != "E00001" || selfResp.OrgHistory[0].RemovedReason != "离职归档" {
		t.Fatalf("expected self orgHistory visible, got %+v", selfResp.OrgHistory)
	}

	// 2. Peer profile request MUST NOT leak OrgHistory or removal reason.
	peerResp, err := srv.GetUserProfile(context.Background(), &contactrpc.GetUserProfileReq{
		OperatorUserID: "u-b",
		TargetUserID:   "u-a",
	})
	if err != nil {
		t.Fatalf("peer GetUserProfile failed: %v", err)
	}
	if len(peerResp.OrgHistory) != 0 {
		t.Fatalf("expected peer GetUserProfile orgHistory to be empty, got %+v", peerResp.OrgHistory)
	}
	if peerResp.Profile.EmployeeNo != "E00001" {
		t.Fatalf("expected peer to still see user employeeNo E00001, got %q", peerResp.Profile.EmployeeNo)
	}

	// 3. External user (not in org) can freely update nickname, email, phone, signature.
	nick, email, phone, sig := "Bob New", "bob@test.com", "13800138000", "独立开发者"
	updResp, err := srv.UpdateUserProfile(context.Background(), &contactrpc.UpdateUserProfileReq{
		UserID:    "u-b",
		Nickname:  &nick,
		Email:     &email,
		Phone:     &phone,
		Signature: &sig,
	})
	if err != nil {
		t.Fatalf("external UpdateUserProfile failed: %v", err)
	}
	if updResp.Profile.Nickname != nick || updResp.Profile.Email != email || updResp.Profile.Phone != phone || updResp.Profile.Signature != sig {
		t.Fatalf("unexpected updated profile: %+v", updResp.Profile)
	}
}

func TestOrgMemberVisibilityGlobalAndPerDeptOverride(t *testing.T) {
	db := &visibilityStubContactDB{
		version:          2001,
		memberVisibility: model.OrgMemberVisibilityOwnDept,
		depts: []*model.OrgDepartment{
			{DeptID: "dept_sales", ParentID: "", Name: "销售部", AncestorPath: "/", MemberVisibility: "", Version: 2001},
			{DeptID: "dept_exec", ParentID: "", Name: "总经办", AncestorPath: "/", MemberVisibility: model.OrgMemberVisibilityAll, Version: 2001},
			{DeptID: "dept_secret", ParentID: "", Name: "保密组", AncestorPath: "/", MemberVisibility: model.OrgMemberVisibilityOwnDept, Version: 2001},
		},
		members: []*model.UserDepartmentMember{
			{DeptID: "dept_sales", UserID: "u-sales"},
			{DeptID: "dept_exec", UserID: "u-exec"},
			{DeptID: "dept_secret", UserID: "u-secret"},
		},
	}
	userCtrl := &profileStubUserCtrl{
		users: map[string]*model.User{
			"u-sales":  {UserID: "u-sales", Account: "1001", Nickname: "Sales", PrimaryDeptIDs: []string{"dept_sales"}},
			"u-exec":   {UserID: "u-exec", Account: "1002", Nickname: "Exec", PrimaryDeptIDs: []string{"dept_exec"}},
			"u-secret": {UserID: "u-secret", Account: "1003", Nickname: "Secret", PrimaryDeptIDs: []string{"dept_secret"}},
		},
	}
	srv := &userServer{contactDB: db, userStorageHandler: userCtrl}

	// 1. Org default = own_dept: Sales (inherits own_dept) sees FULL org tree (3 depts),
	//    can view own dept_sales members, but gets restricted=true on dept_exec & dept_secret.
	salesTree, err := srv.GetOrgTree(context.Background(), &contactrpc.GetOrgTreeReq{OperatorUserID: "u-sales"})
	if err != nil || len(salesTree.Departments) != 3 || salesTree.MemberVisibility != model.OrgMemberVisibilityOwnDept {
		t.Fatalf("sales should see full 3-dept tree with effective own_dept, got %+v err=%v", salesTree, err)
	}
	salesOwn, err := srv.GetDeptMembers(context.Background(), &contactrpc.GetDeptMembersReq{OperatorUserID: "u-sales", DeptID: "dept_sales"})
	if err != nil || salesOwn.Restricted || len(salesOwn.Members) != 1 {
		t.Fatalf("sales should view own dept members, got %+v err=%v", salesOwn, err)
	}
	salesOther, err := srv.GetDeptMembers(context.Background(), &contactrpc.GetDeptMembersReq{OperatorUserID: "u-sales", DeptID: "dept_exec"})
	if err != nil || !salesOther.Restricted || len(salesOther.Members) != 0 {
		t.Fatalf("sales should be restricted on dept_exec, got %+v err=%v", salesOther, err)
	}

	// 2. Exec dept has per-dept override = all: Exec sees full org tree AND all departments' members.
	execTree, err := srv.GetOrgTree(context.Background(), &contactrpc.GetOrgTreeReq{OperatorUserID: "u-exec"})
	if err != nil || len(execTree.Departments) != 3 || execTree.MemberVisibility != model.OrgMemberVisibilityAll {
		t.Fatalf("exec should see full tree with effective all, got %+v err=%v", execTree, err)
	}
	execViewSales, err := srv.GetDeptMembers(context.Background(), &contactrpc.GetDeptMembersReq{OperatorUserID: "u-exec", DeptID: "dept_sales"})
	if err != nil || execViewSales.Restricted || len(execViewSales.Members) != 1 {
		t.Fatalf("exec (override=all) should view dept_sales members, got %+v err=%v", execViewSales, err)
	}

	// 3. Switch org default = all: Sales (inherits) can now view dept_secret,
	//    while Secret (per-dept override=own_dept) STILL cannot view dept_sales!
	db.memberVisibility = model.OrgMemberVisibilityAll
	salesViewSecret, err := srv.GetDeptMembers(context.Background(), &contactrpc.GetDeptMembersReq{OperatorUserID: "u-sales", DeptID: "dept_secret"})
	if err != nil || salesViewSecret.Restricted || len(salesViewSecret.Members) != 1 {
		t.Fatalf("sales (org default=all) should view dept_secret members, got %+v err=%v", salesViewSecret, err)
	}
	secretViewSales, err := srv.GetDeptMembers(context.Background(), &contactrpc.GetDeptMembersReq{OperatorUserID: "u-secret", DeptID: "dept_sales"})
	if err != nil || !secretViewSales.Restricted || len(secretViewSales.Members) != 0 {
		t.Fatalf("secret (override=own_dept) must still be restricted on dept_sales, got %+v err=%v", secretViewSales, err)
	}
}
