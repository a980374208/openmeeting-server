package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/openmeeting-server/pkg/common/securetools"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/openmeeting-server/pkg/common/token"
	"github.com/openimsdk/openmeeting-server/pkg/rpcclient"
	"github.com/openimsdk/tools/errs"
)

type stubUserController struct {
	users           map[string]*model.User
	storedUserToken map[string]string
}

func (s *stubUserController) FindWithError(_ context.Context, userIDs []string) ([]*model.User, error) {
	return s.FindByUserIDs(context.Background(), userIDs)
}
func (s *stubUserController) Create(_ context.Context, users []*model.User) error {
	for _, u := range users {
		s.users[u.UserID] = u
	}
	return nil
}
func (s *stubUserController) GetByAccount(_ context.Context, account string) (*model.User, error) {
	for _, u := range s.users {
		if u.Account == account {
			return u, nil
		}
	}
	return nil, errs.ErrRecordNotFound.WrapMsg("not found")
}
func (s *stubUserController) StoreToken(_ context.Context, userID, tok string) error {
	s.storedUserToken[userID] = tok
	return nil
}
func (s *stubUserController) GetToken(_ context.Context, userID string) (string, error) {
	return s.storedUserToken[userID], nil
}
func (s *stubUserController) ClearUserToken(_ context.Context, userID string) error {
	delete(s.storedUserToken, userID)
	return nil
}
func (s *stubUserController) GenerateUserID(_ context.Context) (string, error) {
	return "u_gen_1", nil
}
func (s *stubUserController) TakeByExactIdentifier(_ context.Context, identifier string) (*model.User, error) {
	return s.GetByAccount(context.Background(), identifier)
}
func (s *stubUserController) FindByUserIDs(_ context.Context, userIDs []string) ([]*model.User, error) {
	var out []*model.User
	for _, id := range userIDs {
		if u, ok := s.users[id]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}
func (s *stubUserController) UpdateProfile(_ context.Context, userID string, _ string, update map[string]any) error {
	u, ok := s.users[userID]
	if !ok {
		return errs.ErrRecordNotFound
	}
	if v, ok := update["primary_dept_ids"].([]string); ok {
		u.PrimaryDeptIDs = v
	}
	if v, ok := update["employee_no"].(string); ok {
		u.EmployeeNo = v
	}
	if v, ok := update["job_title"].(string); ok {
		u.JobTitle = v
	}
	if v, ok := update["search_tokens"].([]string); ok {
		u.SearchTokens = v
	}
	return nil
}
func (s *stubUserController) UpsertByAccount(_ context.Context, user *model.User) error {
	s.users[user.UserID] = user
	return nil
}
func (s *stubUserController) SearchByPrefix(_ context.Context, _ string, _ string, _ []string, _, _ int64) ([]*model.User, int64, error) {
	return s.ListAll(context.Background(), "", 0, 100)
}
func (s *stubUserController) ListAll(_ context.Context, _ string, _, _ int64) ([]*model.User, int64, error) {
	var out []*model.User
	for _, u := range s.users {
		out = append(out, u)
	}
	return out, int64(len(out)), nil
}

type stubContactDB struct {
	depts            []*model.OrgDepartment
	members          []*model.UserDepartmentMember
	empRecords       []*model.OrgEmployeeRecord
	version          int64
	orgCode          string
	memberVisibility string
	deptSeq          int64
	empSeq           int64
	bumpCalls        int
	replaceCalls     int
}

func (s *stubContactDB) ListDepartments(_ context.Context) ([]*model.OrgDepartment, int64, error) {
	return s.depts, s.version, nil
}
func (s *stubContactDB) GetDepartment(_ context.Context, deptID string) (*model.OrgDepartment, error) {
	for _, d := range s.depts {
		if d.DeptID == deptID {
			return d, nil
		}
	}
	return nil, errs.ErrRecordNotFound
}
func (s *stubContactDB) UpsertDepartment(_ context.Context, dept *model.OrgDepartment) error {
	for i, d := range s.depts {
		if d.DeptID == dept.DeptID {
			s.depts[i] = dept
			return nil
		}
	}
	s.depts = append(s.depts, dept)
	return nil
}
func (s *stubContactDB) EnsureDepartmentPath(_ context.Context, _ string) ([]*model.OrgDepartment, error) {
	return nil, nil
}
func (s *stubContactDB) SetUserDepartments(_ context.Context, _ string, _ []string, _ bool) error {
	return nil
}
func (s *stubContactDB) ListDepartmentMembers(_ context.Context, deptID string, _, _ int64) ([]*model.UserDepartmentMember, int64, error) {
	var out []*model.UserDepartmentMember
	for _, m := range s.members {
		if m.DeptID == deptID {
			out = append(out, m)
		}
	}
	return out, int64(len(out)), nil
}
func (s *stubContactDB) ListAllDepartmentMembers(_ context.Context) ([]*model.UserDepartmentMember, error) {
	return s.members, nil
}
func (s *stubContactDB) ListUserDepartments(_ context.Context, userID string) ([]*model.UserDepartmentMember, error) {
	var out []*model.UserDepartmentMember
	for _, m := range s.members {
		if m.UserID == userID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (s *stubContactDB) ReplaceOrgSnapshot(_ context.Context, depts []*model.OrgDepartment, members []*model.UserDepartmentMember, minVersion int64) (int64, error) {
	s.replaceCalls++
	next := minVersion
	if next <= s.version {
		next = s.version + 1
	}
	for _, d := range depts {
		d.Version = next
		d.UpdatedAt = next
	}
	s.depts = depts
	s.members = members
	s.version = next
	return next, nil
}
func (s *stubContactDB) BumpOrgTreeVersion(_ context.Context, minVersion int64) (int64, error) {
	s.bumpCalls++
	if minVersion > s.version {
		s.version = minVersion
	} else {
		s.version++
	}
	return s.version, nil
}
func (s *stubContactDB) GetOrgMemberVisibility(_ context.Context) (string, error) {
	return model.NormalizeOrgMemberVisibility(s.memberVisibility), nil
}
func (s *stubContactDB) SetOrgMemberVisibility(_ context.Context, visibility string) error {
	s.memberVisibility = model.NormalizeOrgMemberVisibility(visibility)
	return nil
}
func (s *stubContactDB) EnsureOrgCode(_ context.Context) (string, error) {
	if s.orgCode == "" {
		s.orgCode = "O00001"
	}
	return s.orgCode, nil
}
func (s *stubContactDB) AllocateDeptCode(_ context.Context) (string, error) {
	s.deptSeq++
	return fmt.Sprintf("D%05d", s.deptSeq), nil
}
func (s *stubContactDB) AllocateEmployeeNo(_ context.Context) (string, error) {
	s.empSeq++
	return fmt.Sprintf("E%05d", s.empSeq), nil
}
func (s *stubContactDB) CreateEmployeeRecord(_ context.Context, rec *model.OrgEmployeeRecord) error {
	if rec.RecordID == "" {
		rec.RecordID = fmt.Sprintf("rec_%d", len(s.empRecords)+1)
	}
	cp := *rec
	s.empRecords = append(s.empRecords, &cp)
	return nil
}
func (s *stubContactDB) UpdateActiveEmployeeRecord(_ context.Context, userID, nickname, jobTitle string, deptNames []string) error {
	for _, rec := range s.empRecords {
		if rec.UserID == userID && rec.Status == model.OrgEmployeeStatusActive {
			if nickname != "" {
				rec.Nickname = nickname
			}
			rec.JobTitle = jobTitle
			rec.DeptNames = append([]string(nil), deptNames...)
		}
	}
	return nil
}
func (s *stubContactDB) MarkEmployeeRecordRemoved(_ context.Context, userID string, removedAt int64, reason, removedByUserID, removedByAccount string, lastDeptNames []string) (int64, error) {
	var count int64
	for _, rec := range s.empRecords {
		if rec.UserID == userID && rec.Status == model.OrgEmployeeStatusActive {
			rec.Status = model.OrgEmployeeStatusRemoved
			rec.RemovedAt = removedAt
			rec.RemovedReason = reason
			rec.RemovedByUserID = removedByUserID
			rec.RemovedByAccount = removedByAccount
			if len(lastDeptNames) > 0 {
				rec.DeptNames = append([]string(nil), lastDeptNames...)
			}
			count++
		}
	}
	return count, nil
}
func (s *stubContactDB) ListUserEmployeeRecords(_ context.Context, userID string) ([]*model.OrgEmployeeRecord, error) {
	var out []*model.OrgEmployeeRecord
	for i := len(s.empRecords) - 1; i >= 0; i-- {
		if s.empRecords[i].UserID == userID {
			out = append(out, s.empRecords[i])
		}
	}
	return out, nil
}
func (s *stubContactDB) ListAllEmployeeRecords(_ context.Context) ([]*model.OrgEmployeeRecord, error) {
	out := make([]*model.OrgEmployeeRecord, 0, len(s.empRecords))
	for i := len(s.empRecords) - 1; i >= 0; i-- {
		out = append(out, s.empRecords[i])
	}
	return out, nil
}
func (s *stubContactDB) AreInSameOrgDirectory(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}
func (s *stubContactDB) ListRelations(_ context.Context, _ string, _ int64) ([]*model.ContactRelation, error) {
	return nil, nil
}
func (s *stubContactDB) GetRelation(_ context.Context, _, _ string) (*model.ContactRelation, error) {
	return nil, errs.ErrRecordNotFound
}
func (s *stubContactDB) UpsertRelation(_ context.Context, _ *model.ContactRelation) error { return nil }
func (s *stubContactDB) RemoveRelation(_ context.Context, _, _ string) error             { return nil }
func (s *stubContactDB) IsBlocked(_ context.Context, _, _ string) (bool, error)          { return false, nil }
func (s *stubContactDB) ListGroups(_ context.Context, _ string) ([]*model.ContactGroup, error) {
	return nil, nil
}
func (s *stubContactDB) UpsertGroup(_ context.Context, _ *model.ContactGroup) error { return nil }
func (s *stubContactDB) DeleteGroup(_ context.Context, _, _ string) error           { return nil }
func (s *stubContactDB) CreateRequest(_ context.Context, _ *model.ContactRequest) error {
	return nil
}
func (s *stubContactDB) GetRequest(_ context.Context, _ string) (*model.ContactRequest, error) {
	return nil, errs.ErrRecordNotFound
}
func (s *stubContactDB) FindPendingRequest(_ context.Context, _, _ string) (*model.ContactRequest, error) {
	return nil, nil
}
func (s *stubContactDB) UpdateRequestStatus(_ context.Context, _ string, _, _ int32, _ int64) (bool, error) {
	return false, nil
}
func (s *stubContactDB) ListRequests(_ context.Context, _ string, _ int64) ([]*model.ContactRequest, error) {
	return nil, nil
}

func makeSeededUser(id, account, nickname, rawPass string) *model.User {
	hash, salt := securetools.HashPassword(rawPass)
	return &model.User{
		UserID:    id,
		Account:   account,
		Nickname:  nickname,
		Password:  hash,
		SaltValue: salt,
	}
}

func TestFounderAuthAndOrgApply(t *testing.T) {
	gin.SetMode(gin.TestMode)

	founder := makeSeededUser("u-founder", "10000", "Founder", "pass123")
	normal := makeSeededUser("u-normal", "10001", "Normal", "pass123")
	normal.Email = "normal@example.com"
	normal.Signature = "hello world"

	userCtrl := &stubUserController{
		users: map[string]*model.User{
			founder.UserID: founder,
			normal.UserID:  normal,
		},
		storedUserToken: map[string]string{
			founder.UserID: "desktop-client-jwt-must-remain-untouched",
		},
	}
	contactDB := &stubContactDB{}
	tok := token.New(1, "admin-test-secret")
	apiAdmin := NewAdminApi(userCtrl, contactDB, nil, nil, rpcclient.User{}, tok, []string{"10000"})

	r := gin.New()
	r.Use(gin.Recovery(), adminCorsHandler)
	registerAdminRoutes(r, apiAdmin)

	// 1. Embedded Web UI is served without auth or operationID
	for _, tc := range []struct {
		path string
		want string
	}{
		{"/", "text/html"},
		{"/admin/ui/app.js", "application/javascript"},
		{"/admin/ui/app.css", "text/css"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d want 200", tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !bytes.Contains([]byte(ct), []byte(tc.want)) {
			t.Fatalf("GET %s content-type=%q want substring %q", tc.path, ct, tc.want)
		}
	}

	// 2. Non-founder login must be rejected
	{
		body := bytes.NewBufferString(`{"account":"10001","password":"pass123"}`)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/login", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		var resp struct {
			ErrCode int `json:"errCode"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.ErrCode == 0 {
			t.Fatalf("expected non-founder login to fail, got body=%s", rec.Body.String())
		}
	}

	// 3. Founder login succeeds and does NOT overwrite desktop client token
	var adminJWT string
	{
		body := bytes.NewBufferString(`{"account":"10000","password":"pass123"}`)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/login", body)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		var resp struct {
			ErrCode int `json:"errCode"`
			Data    struct {
				AdminToken string `json:"adminToken"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ErrCode != 0 || resp.Data.AdminToken == "" {
			t.Fatalf("founder login failed: %s", rec.Body.String())
		}
		adminJWT = resp.Data.AdminToken
		if userCtrl.storedUserToken[founder.UserID] != "desktop-client-jwt-must-remain-untouched" {
			t.Fatalf("admin login unexpectedly modified desktop client token!")
		}
	}

	// 4. Unauthenticated snapshot request rejected with 401
	{
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/org/snapshot", nil)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated snapshot, got %d", rec.Code)
		}
	}

	// 5. Apply multi-level org snapshot: auto-assigns O00001, D00001/D00002, E00001; ignores any forged employeeNo override.
	var dept1ID, dept2ID string
	{
		payload := `{
			"departments": [
				{"deptID": "tmp_1", "parentID": "", "name": "总部", "sortOrder": 1},
				{"deptID": "tmp_2", "parentID": "tmp_1", "name": "研发部", "sortOrder": 2}
			],
			"members": [
				{"deptID": "tmp_2", "userID": "u-normal", "isLeader": true, "sortOrder": 0}
			],
			"userUpdates": [
				{"userID": "u-normal", "employeeNo": "FORGED999", "jobTitle": "架构师"}
			]
		}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/org/apply", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("token", adminJWT)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("apply failed HTTP %d: %s", rec.Code, rec.Body.String())
		}
		var resp struct {
			ErrCode int `json:"errCode"`
			Data    struct {
				OrgCode         string                     `json:"orgCode"`
				TreeVersion     int64                      `json:"treeVersion"`
				Departments     []orgDeptPayload           `json:"departments"`
				EmployeeRecords []orgEmployeeRecordPayload `json:"employeeRecords"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ErrCode != 0 {
			t.Fatalf("apply resp error: %s", rec.Body.String())
		}
		if resp.Data.OrgCode != "O00001" {
			t.Fatalf("expected orgCode O00001, got %q", resp.Data.OrgCode)
		}
		if len(resp.Data.Departments) != 2 || resp.Data.TreeVersion <= 0 {
			t.Fatalf("unexpected applied snapshot: %+v", resp.Data)
		}
		if resp.Data.Departments[0].DeptCode != "D00001" || resp.Data.Departments[1].DeptCode != "D00002" {
			t.Fatalf("expected dept codes D00001/D00002, got %+v", resp.Data.Departments)
		}
		dept1ID = resp.Data.Departments[0].DeptID
		dept2ID = resp.Data.Departments[1].DeptID
		if contactDB.bumpCalls != 0 || contactDB.replaceCalls != 1 || resp.Data.TreeVersion != contactDB.version {
			t.Fatalf("expected single version bump inside ReplaceOrgSnapshot, bumpCalls=%d replaceCalls=%d respVer=%d dbVer=%d",
				contactDB.bumpCalls, contactDB.replaceCalls, resp.Data.TreeVersion, contactDB.version)
		}
		if normal.EmployeeNo != "E00001" {
			t.Fatalf("expected auto-assigned immutable employeeNo E00001, got %q", normal.EmployeeNo)
		}
		if len(normal.PrimaryDeptIDs) != 1 || normal.JobTitle != "架构师" {
			t.Fatalf("expected normal user primary_dept_ids & jobTitle updated, got depts=%v title=%q", normal.PrimaryDeptIDs, normal.JobTitle)
		}
		if len(founder.PrimaryDeptIDs) != 0 || founder.EmployeeNo != "" {
			t.Fatalf("founder not in org should have empty depts & empty employeeNo, got %v %q", founder.PrimaryDeptIDs, founder.EmployeeNo)
		}
	}

	// 6. Cyclic hierarchy must be rejected
	{
		_, _, _, err := normalizeOrgSnapshotInput([]orgDeptPayload{
			{DeptID: "d1", ParentID: "d2", Name: "A"},
			{DeptID: "d2", ParentID: "d1", Name: "B"},
		}, nil, 100)
		if err == nil {
			t.Fatalf("expected cyclic department hierarchy to be rejected")
		}
	}

	// 7. Remove u-normal with optional removal reason, while adding u-founder:
	//    - u-normal keeps E00001 + email + signature, record archived with reason
	//    - u-founder gets E00002 (never reuses E00001)
	{
		payload := fmt.Sprintf(`{
			"departments": [
				{"deptID": %q, "parentID": "", "name": "总部", "sortOrder": 1},
				{"deptID": %q, "parentID": %q, "name": "研发部", "sortOrder": 2}
			],
			"members": [
				{"deptID": %q, "userID": "u-founder", "isLeader": true, "sortOrder": 0}
			],
			"removalReasons": {
				"u-normal": "项目调岗离组"
			}
		}`, dept1ID, dept2ID, dept1ID, dept2ID)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/org/apply", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("token", adminJWT)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("remove+add apply failed: %s", rec.Body.String())
		}
		if normal.EmployeeNo != "E00001" || normal.Email != "normal@example.com" || normal.Signature != "hello world" {
			t.Fatalf("removed user should keep employeeNo & personal profile, got emp=%q email=%q sig=%q",
				normal.EmployeeNo, normal.Email, normal.Signature)
		}
		if len(normal.PrimaryDeptIDs) != 0 {
			t.Fatalf("expected removed user primary_dept_ids empty, got %v", normal.PrimaryDeptIDs)
		}
		if founder.EmployeeNo != "E00002" {
			t.Fatalf("expected founder to receive never-reused E00002, got %q", founder.EmployeeNo)
		}
		recs, _ := contactDB.ListUserEmployeeRecords(context.Background(), "u-normal")
		if len(recs) != 1 || recs[0].Status != model.OrgEmployeeStatusRemoved || recs[0].RemovedReason != "项目调岗离组" || recs[0].EmployeeNo != "E00001" {
			t.Fatalf("unexpected archived record for u-normal: %+v", recs)
		}
	}

	// 8. Re-add u-normal into org: must receive BRAND NEW E00003 (never reusing E00001!),
	//    while E00001 archive record + removal reason remain permanently stored.
	{
		payload := fmt.Sprintf(`{
			"departments": [
				{"deptID": %q, "parentID": "", "name": "总部", "sortOrder": 1},
				{"deptID": %q, "parentID": %q, "name": "研发部", "sortOrder": 2}
			],
			"members": [
				{"deptID": %q, "userID": "u-founder", "isLeader": true, "sortOrder": 0},
				{"deptID": %q, "userID": "u-normal", "isLeader": false, "sortOrder": 1}
			]
		}`, dept1ID, dept2ID, dept1ID, dept2ID, dept2ID)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/org/apply", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("token", adminJWT)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("re-join apply failed: %s", rec.Body.String())
		}
		if normal.EmployeeNo != "E00003" {
			t.Fatalf("expected re-joining user to receive brand new E00003 (never reusing E00001), got %q", normal.EmployeeNo)
		}
		if founder.EmployeeNo != "E00002" {
			t.Fatalf("staying member must keep immutable E00002, got %q", founder.EmployeeNo)
		}
		recs, _ := contactDB.ListUserEmployeeRecords(context.Background(), "u-normal")
		if len(recs) != 2 {
			t.Fatalf("expected 2 records (active E00003 + archived E00001), got %d", len(recs))
		}
		if recs[0].EmployeeNo != "E00003" || recs[0].Status != model.OrgEmployeeStatusActive {
			t.Fatalf("expected newest record active E00003, got %+v", recs[0])
		}
		if recs[1].EmployeeNo != "E00001" || recs[1].Status != model.OrgEmployeeStatusRemoved || recs[1].RemovedReason != "项目调岗离组" {
			t.Fatalf("expected historical archived record E00001 intact, got %+v", recs[1])
		}
	}

	// 9. Founder configures org-wide default visibility (own_dept) AND per-department override (all on dept2)
	{
		payload := fmt.Sprintf(`{
			"memberVisibility": "own_dept",
			"departments": [
				{"deptID": %q, "parentID": "", "name": "总部", "sortOrder": 1, "memberVisibility": ""},
				{"deptID": %q, "parentID": %q, "name": "研发部", "sortOrder": 2, "memberVisibility": "all"}
			],
			"members": [
				{"deptID": %q, "userID": "u-founder", "isLeader": true, "sortOrder": 0},
				{"deptID": %q, "userID": "u-normal", "isLeader": false, "sortOrder": 1}
			]
		}`, dept1ID, dept2ID, dept1ID, dept2ID, dept2ID)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/org/apply", bytes.NewBufferString(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("token", adminJWT)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("visibility apply failed: %s", rec.Body.String())
		}
		var resp struct {
			ErrCode int `json:"errCode"`
			Data    struct {
				MemberVisibility string           `json:"memberVisibility"`
				Departments      []orgDeptPayload `json:"departments"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ErrCode != 0 {
			t.Fatalf("visibility resp err: %s", rec.Body.String())
		}
		if resp.Data.MemberVisibility != model.OrgMemberVisibilityOwnDept {
			t.Fatalf("expected org memberVisibility=own_dept, got %q", resp.Data.MemberVisibility)
		}
		if resp.Data.Departments[0].MemberVisibility != "" || resp.Data.Departments[1].MemberVisibility != model.OrgMemberVisibilityAll {
			t.Fatalf("unexpected dept visibility overrides: %+v", resp.Data.Departments)
		}
	}
}
