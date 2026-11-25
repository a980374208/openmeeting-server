package admin

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
)

type orgDeptPayload struct {
	DeptID           string `json:"deptID"`
	DeptCode         string `json:"deptCode,omitempty"`
	ParentID         string `json:"parentID"`
	Name             string `json:"name"`
	AncestorPath     string `json:"ancestorPath,omitempty"`
	SortOrder        int32  `json:"sortOrder"`
	MemberVisibility string `json:"memberVisibility,omitempty"`
	Version          int64  `json:"version,omitempty"`
}

type orgMemberPayload struct {
	DeptID    string `json:"deptID"`
	UserID    string `json:"userID"`
	IsLeader  bool   `json:"isLeader"`
	SortOrder int32  `json:"sortOrder"`
}

type adminUserPayload struct {
	UserID         string   `json:"userID"`
	Account        string   `json:"account"`
	Nickname       string   `json:"nickname"`
	EmployeeNo     string   `json:"employeeNo"`
	Email          string   `json:"email"`
	Phone          string   `json:"phone"`
	JobTitle       string   `json:"jobTitle"`
	Signature      string   `json:"signature"`
	PrimaryDeptIDs []string `json:"primaryDeptIDs"`
	InOrgDirectory bool     `json:"inOrgDirectory"`
	Status         int32    `json:"status"`
}

type orgEmployeeRecordPayload struct {
	RecordID         string   `json:"recordID"`
	OrgCode          string   `json:"orgCode,omitempty"`
	EmployeeNo       string   `json:"employeeNo"`
	UserID           string   `json:"userID"`
	Account          string   `json:"account"`
	Nickname         string   `json:"nickname"`
	JobTitle         string   `json:"jobTitle,omitempty"`
	DeptNames        []string `json:"deptNames,omitempty"`
	Status           int32    `json:"status"` // 1=active, 2=removed
	JoinedAtMs       int64    `json:"joinedAtMs"`
	RemovedAtMs      int64    `json:"removedAtMs,omitempty"`
	RemovedReason    string   `json:"removedReason,omitempty"`
	RemovedByAccount string   `json:"removedByAccount,omitempty"`
}

// adminUserUpdateInput limits founder-side profile writes strictly to jobTitle.
// EmployeeNo is system-allocated on org entry and immutable; personal profile fields
// (nickname, email, phone, signature) are self-managed by users whether in org or not.
type adminUserUpdateInput struct {
	UserID   string  `json:"userID"`
	JobTitle *string `json:"jobTitle,omitempty"`
}

type applyOrgSnapshotReq struct {
	Departments      []orgDeptPayload       `json:"departments"`
	Members          []orgMemberPayload     `json:"members"`
	UserUpdates      []adminUserUpdateInput `json:"userUpdates,omitempty"`
	RemovalReasons   map[string]string      `json:"removalReasons,omitempty"`
	MemberVisibility *string                `json:"memberVisibility,omitempty"`
}

func truncateRunes(s string, maxLen int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > maxLen {
		return string(r[:maxLen])
	}
	return string(r)
}

func (a *ApiAdmin) currentFounder(c *gin.Context) (string, string) {
	if raw, ok := c.Get(founderContextKey); ok {
		if u, ok := raw.(*model.User); ok && u != nil {
			return u.UserID, u.Account
		}
	}
	return "", ""
}

// ensureExistingOrgBackfill ensures pre-existing departments and org members created
// before auto-numbering have immutable orgCode (O00001+), deptCode (D00001+),
// employeeNo (E00001+), and active OrgEmployeeRecord entries.
func (a *ApiAdmin) ensureExistingOrgBackfill(
	c *gin.Context,
	depts []*model.OrgDepartment,
	members []*model.UserDepartmentMember,
	users []*model.User,
) (string, error) {
	if a.contactDB == nil {
		return "", nil
	}
	orgCode := ""
	if len(depts) > 0 {
		code, err := a.contactDB.EnsureOrgCode(c)
		if err != nil {
			return "", err
		}
		orgCode = code
	}

	validDepts := make(map[string]string, len(depts))
	for _, d := range depts {
		if strings.TrimSpace(d.DeptCode) == "" {
			code, err := a.contactDB.AllocateDeptCode(c)
			if err != nil {
				return orgCode, err
			}
			d.DeptCode = code
			_ = a.contactDB.UpsertDepartment(c, d)
		}
		validDepts[d.DeptID] = d.Name
	}

	userDeptNames := make(map[string][]string)
	for _, m := range members {
		if name, ok := validDepts[m.DeptID]; ok {
			userDeptNames[m.UserID] = append(userDeptNames[m.UserID], name)
		}
	}
	if len(userDeptNames) == 0 {
		return orgCode, nil
	}

	existingRecs, err := a.contactDB.ListAllEmployeeRecords(c)
	if err != nil {
		return orgCode, err
	}
	hasActiveRec := make(map[string]bool, len(existingRecs))
	for _, rec := range existingRecs {
		if rec.Status == model.OrgEmployeeStatusActive {
			hasActiveRec[rec.UserID] = true
		}
	}

	now := time.Now().UnixMilli()
	for _, u := range users {
		dNames := userDeptNames[u.UserID]
		if len(dNames) == 0 {
			continue
		}
		if strings.TrimSpace(u.EmployeeNo) == "" {
			empNo, err := a.contactDB.AllocateEmployeeNo(c)
			if err != nil {
				return orgCode, err
			}
			u.EmployeeNo = empNo
			u.SearchTokens = model.BuildSearchTokens(u, dNames...)
			if err := a.userStorageHandler.UpdateProfile(c, u.UserID, u.Account, map[string]any{
				"employee_no":   empNo,
				"search_tokens": u.SearchTokens,
			}); err != nil {
				return orgCode, err
			}
		}
		if !hasActiveRec[u.UserID] && strings.TrimSpace(u.EmployeeNo) != "" {
			_ = a.contactDB.CreateEmployeeRecord(c, &model.OrgEmployeeRecord{
				OrgCode:    orgCode,
				EmployeeNo: u.EmployeeNo,
				UserID:     u.UserID,
				Account:    u.Account,
				Nickname:   u.Nickname,
				JobTitle:   u.JobTitle,
				DeptNames:  dNames,
				Status:     model.OrgEmployeeStatusActive,
				JoinedAt:   now,
			})
			hasActiveRec[u.UserID] = true
		}
	}

	return orgCode, nil
}

func (a *ApiAdmin) buildOrgSnapshotData(c *gin.Context) (gin.H, error) {
	if a.contactDB == nil {
		return nil, errs.ErrInternalServer.WrapMsg("contact storage unavailable")
	}
	depts, version, err := a.contactDB.ListDepartments(c)
	if err != nil {
		return nil, err
	}
	members, err := a.contactDB.ListAllDepartmentMembers(c)
	if err != nil {
		return nil, err
	}
	users, _, err := a.userStorageHandler.ListAll(c, "", 0, 1000)
	if err != nil {
		return nil, err
	}

	orgCode, err := a.ensureExistingOrgBackfill(c, depts, members, users)
	if err != nil {
		return nil, err
	}

	deptList := make([]orgDeptPayload, 0, len(depts))
	validDepts := make(map[string]struct{}, len(depts))
	for _, d := range depts {
		validDepts[d.DeptID] = struct{}{}
		deptList = append(deptList, orgDeptPayload{
			DeptID:           d.DeptID,
			DeptCode:         d.DeptCode,
			ParentID:         d.ParentID,
			Name:             d.Name,
			AncestorPath:     d.AncestorPath,
			SortOrder:        d.SortOrder,
			MemberVisibility: model.NormalizeDeptMemberVisibility(d.MemberVisibility),
			Version:          d.Version,
		})
	}

	memberList := make([]orgMemberPayload, 0, len(members))
	userInOrg := make(map[string][]string)
	for _, m := range members {
		if _, ok := validDepts[m.DeptID]; !ok {
			continue
		}
		memberList = append(memberList, orgMemberPayload{
			DeptID:    m.DeptID,
			UserID:    m.UserID,
			IsLeader:  m.IsLeader,
			SortOrder: m.SortOrder,
		})
		userInOrg[m.UserID] = append(userInOrg[m.UserID], m.DeptID)
	}

	userList := make([]adminUserPayload, 0, len(users))
	for _, u := range users {
		deptsForUser := userInOrg[u.UserID]
		if deptsForUser == nil {
			deptsForUser = []string{}
		}
		userList = append(userList, adminUserPayload{
			UserID:         u.UserID,
			Account:        u.Account,
			Nickname:       u.Nickname,
			EmployeeNo:     u.EmployeeNo,
			Email:          u.Email,
			Phone:          u.Phone,
			JobTitle:       u.JobTitle,
			Signature:      u.Signature,
			PrimaryDeptIDs: deptsForUser,
			InOrgDirectory: len(deptsForUser) > 0,
			Status:         u.Status,
		})
	}

	rawRecs, err := a.contactDB.ListAllEmployeeRecords(c)
	if err != nil {
		return nil, err
	}
	recordList := make([]orgEmployeeRecordPayload, 0, len(rawRecs))
	for _, rec := range rawRecs {
		recordList = append(recordList, orgEmployeeRecordPayload{
			RecordID:         rec.RecordID,
			OrgCode:          rec.OrgCode,
			EmployeeNo:       rec.EmployeeNo,
			UserID:           rec.UserID,
			Account:          rec.Account,
			Nickname:         rec.Nickname,
			JobTitle:         rec.JobTitle,
			DeptNames:        append([]string(nil), rec.DeptNames...),
			Status:           rec.Status,
			JoinedAtMs:       rec.JoinedAt,
			RemovedAtMs:      rec.RemovedAt,
			RemovedReason:    rec.RemovedReason,
			RemovedByAccount: rec.RemovedByAccount,
		})
	}

	memberVisibility, err := a.contactDB.GetOrgMemberVisibility(c)
	if err != nil {
		memberVisibility = model.OrgMemberVisibilityAll
	}
	memberVisibility = model.NormalizeOrgMemberVisibility(memberVisibility)

	return gin.H{
		"orgCode":          orgCode,
		"treeVersion":      version,
		"memberVisibility": memberVisibility,
		"departments":      deptList,
		"members":          memberList,
		"users":            userList,
		"employeeRecords":  recordList,
	}, nil
}

func (a *ApiAdmin) GetOrgSnapshot(c *gin.Context) {
	data, err := a.buildOrgSnapshotData(c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, data)
}

func normalizeOrgSnapshotInput(
	rawDepts []orgDeptPayload,
	rawMembers []orgMemberPayload,
	version int64,
) ([]*model.OrgDepartment, []*model.UserDepartmentMember, map[string]string, error) {
	idRemap := make(map[string]string, len(rawDepts))
	deptByID := make(map[string]*model.OrgDepartment, len(rawDepts))
	ordered := make([]*model.OrgDepartment, 0, len(rawDepts))

	// First pass: assign stable department IDs (remap client-generated tmp_ IDs).
	for _, d := range rawDepts {
		rawID := strings.TrimSpace(d.DeptID)
		name := strings.TrimSpace(d.Name)
		if name == "" || len([]rune(name)) > 64 {
			return nil, nil, nil, errs.ErrArgs.WrapMsg("department name must be 1..64 characters")
		}
		finalID := rawID
		if finalID == "" || strings.HasPrefix(finalID, "tmp_") {
			finalID = "dept_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
		}
		if rawID != "" {
			if _, dup := idRemap[rawID]; dup {
				return nil, nil, nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("duplicate department ID %q", rawID))
			}
			idRemap[rawID] = finalID
		}
		if _, dup := deptByID[finalID]; dup {
			return nil, nil, nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("duplicate department ID %q", finalID))
		}
		doc := &model.OrgDepartment{
			DeptID:           finalID,
			ParentID:         strings.TrimSpace(d.ParentID),
			Name:             name,
			SortOrder:        d.SortOrder,
			MemberVisibility: model.NormalizeDeptMemberVisibility(strings.TrimSpace(d.MemberVisibility)),
			Version:          version,
		}
		deptByID[finalID] = doc
		ordered = append(ordered, doc)
	}

	// Second pass: remap parentIDs, validate parent existence, sibling name uniqueness, and acyclicity.
	siblingNames := make(map[string]struct{}, len(ordered))
	for _, d := range ordered {
		if d.ParentID != "" {
			if mapped, ok := idRemap[d.ParentID]; ok {
				d.ParentID = mapped
			}
			if d.ParentID == d.DeptID {
				return nil, nil, nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("department %q cannot be its own parent", d.Name))
			}
			if _, ok := deptByID[d.ParentID]; !ok {
				return nil, nil, nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("department %q references missing parent %q", d.Name, d.ParentID))
			}
		}
		sibKey := d.ParentID + "\x00" + strings.ToLower(d.Name)
		if _, dup := siblingNames[sibKey]; dup {
			return nil, nil, nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("duplicate sibling department name %q", d.Name))
		}
		siblingNames[sibKey] = struct{}{}
	}

	// Third pass: detect cycles and compute canonical ancestor_path ("/" or "/root/sub/").
	var computePath func(id string, visiting map[string]bool) (string, error)
	computed := make(map[string]string, len(ordered))
	computePath = func(id string, visiting map[string]bool) (string, error) {
		if p, ok := computed[id]; ok {
			return p, nil
		}
		if visiting[id] {
			return "", errs.ErrArgs.WrapMsg("department hierarchy contains a cycle")
		}
		visiting[id] = true
		d := deptByID[id]
		var path string
		if d.ParentID == "" {
			path = "/"
		} else {
			parentAnc, err := computePath(d.ParentID, visiting)
			if err != nil {
				return "", err
			}
			path = parentAnc + d.ParentID + "/"
		}
		delete(visiting, id)
		computed[id] = path
		d.AncestorPath = path
		return path, nil
	}
	for _, d := range ordered {
		if _, err := computePath(d.DeptID, make(map[string]bool)); err != nil {
			return nil, nil, nil, err
		}
	}

	// Normalize & deduplicate members.
	seenPair := make(map[string]struct{}, len(rawMembers))
	members := make([]*model.UserDepartmentMember, 0, len(rawMembers))
	for _, m := range rawMembers {
		deptID := strings.TrimSpace(m.DeptID)
		if mapped, ok := idRemap[deptID]; ok {
			deptID = mapped
		}
		userID := strings.TrimSpace(m.UserID)
		if deptID == "" || userID == "" {
			continue
		}
		if _, ok := deptByID[deptID]; !ok {
			return nil, nil, nil, errs.ErrArgs.WrapMsg(fmt.Sprintf("member references unknown department %q", deptID))
		}
		pairKey := deptID + "\x00" + userID
		if _, dup := seenPair[pairKey]; dup {
			continue
		}
		seenPair[pairKey] = struct{}{}
		members = append(members, &model.UserDepartmentMember{
			DeptID:    deptID,
			UserID:    userID,
			IsLeader:  m.IsLeader,
			SortOrder: m.SortOrder,
		})
	}

	return ordered, members, idRemap, nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (a *ApiAdmin) ApplyOrgSnapshot(c *gin.Context) {
	if a.contactDB == nil {
		apiresp.GinError(c, errs.ErrInternalServer.WrapMsg("contact storage unavailable"))
		return
	}
	var req applyOrgSnapshotReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid org snapshot payload: "+err.Error()))
		return
	}

	newVersion := time.Now().UnixMilli()
	depts, members, _, err := normalizeOrgSnapshotInput(req.Departments, req.Members, newVersion)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}

	// Load previous departments & members BEFORE replacement to accurately detect org join/leave transitions
	// and preserve immutable department codes.
	prevDepts, _, err := a.contactDB.ListDepartments(c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	prevMembers, err := a.contactDB.ListAllDepartmentMembers(c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	prevDeptCodeByID := make(map[string]string, len(prevDepts))
	prevDeptNameByID := make(map[string]string, len(prevDepts))
	for _, d := range prevDepts {
		prevDeptCodeByID[d.DeptID] = d.DeptCode
		prevDeptNameByID[d.DeptID] = d.Name
	}
	prevDeptsByUser := make(map[string][]string)
	for _, m := range prevMembers {
		if _, ok := prevDeptNameByID[m.DeptID]; ok {
			prevDeptsByUser[m.UserID] = append(prevDeptsByUser[m.UserID], m.DeptID)
		}
	}

	// Ensure immutable orgCode (O00001+) and department codes (D00001+, never reused).
	orgCode := ""
	if len(depts) > 0 || len(prevDepts) > 0 {
		code, err := a.contactDB.EnsureOrgCode(c)
		if err != nil {
			apiresp.GinError(c, err)
			return
		}
		orgCode = code
	}
	for _, d := range depts {
		if existingCode := strings.TrimSpace(prevDeptCodeByID[d.DeptID]); existingCode != "" {
			d.DeptCode = existingCode
		} else {
			code, err := a.contactDB.AllocateDeptCode(c)
			if err != nil {
				apiresp.GinError(c, err)
				return
			}
			d.DeptCode = code
		}
	}

	// Verify all referenced users exist and load current users to compute org/jobTitle transitions.
	allUsers, _, err := a.userStorageHandler.ListAll(c, "", 0, 1000)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	userByID := make(map[string]*model.User, len(allUsers))
	for _, u := range allUsers {
		userByID[u.UserID] = u
	}

	deptNameByID := make(map[string]string, len(depts))
	for _, d := range depts {
		deptNameByID[d.DeptID] = d.Name
	}
	newDeptsByUser := make(map[string][]string)
	for _, m := range members {
		if _, ok := userByID[m.UserID]; !ok {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg(fmt.Sprintf("user %q does not exist", m.UserID)))
			return
		}
		newDeptsByUser[m.UserID] = append(newDeptsByUser[m.UserID], m.DeptID)
	}
	for uid := range newDeptsByUser {
		sort.Strings(newDeptsByUser[uid])
	}

	// Map explicit founder jobTitle updates (employeeNo and personal fields are NOT writable here).
	userUpdateByID := make(map[string]adminUserUpdateInput, len(req.UserUpdates))
	for _, upd := range req.UserUpdates {
		uid := strings.TrimSpace(upd.UserID)
		if uid != "" {
			if _, ok := userByID[uid]; ok {
				userUpdateByID[uid] = upd
			}
		}
	}

	// Persist optional org-wide default member visibility policy.
	if req.MemberVisibility != nil {
		rawVis := strings.TrimSpace(*req.MemberVisibility)
		if rawVis != model.OrgMemberVisibilityAll && rawVis != model.OrgMemberVisibilityOwnDept {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("memberVisibility must be 'all' or 'own_dept'"))
			return
		}
		if err := a.contactDB.SetOrgMemberVisibility(c, rawVis); err != nil {
			apiresp.GinError(c, err)
			return
		}
	}

	// Persist org snapshot and bump monotonic tree version exactly once.
	appliedVersion, err := a.contactDB.ReplaceOrgSnapshot(c, depts, members, newVersion)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}

	founderUserID, founderAccount := a.currentFounder(c)
	now := time.Now().UnixMilli()
	updatedUsersCount := 0

	for _, u := range allUsers {
		oldSorted := append([]string(nil), u.PrimaryDeptIDs...)
		sort.Strings(oldSorted)
		nextDepts := newDeptsByUser[u.UserID]
		if nextDepts == nil {
			nextDepts = []string{}
		}
		wasInOrg := len(prevDeptsByUser[u.UserID]) > 0
		nowInOrg := len(nextDepts) > 0
		deptsChanged := !stringSlicesEqual(oldSorted, nextDepts)

		updInput, hasJobUpd := userUpdateByID[u.UserID]
		updateFields := make(map[string]any)
		if deptsChanged {
			u.PrimaryDeptIDs = nextDepts
			updateFields["primary_dept_ids"] = nextDepts
		}
		jobTitleChanged := false
		if hasJobUpd && updInput.JobTitle != nil {
			jt := truncateRunes(*updInput.JobTitle, 64)
			if jt != u.JobTitle {
				u.JobTitle = jt
				updateFields["job_title"] = jt
				jobTitleChanged = true
			}
		}

		deptNames := make([]string, 0, len(nextDepts))
		for _, did := range nextDepts {
			if nm := deptNameByID[did]; nm != "" {
				deptNames = append(deptNames, nm)
			}
		}

		// Handle org membership transitions & immutable never-reused employee number lifecycle:
		// 1. Entering or re-entering org (!wasInOrg && nowInOrg): always allocate a BRAND NEW employeeNo.
		// 2. Staying in org (wasInOrg && nowInOrg): keep existing employeeNo (or backfill if empty).
		// 3. Leaving org (wasInOrg && !nowInOrg): preserve employeeNo & profile on user, archive record + optional reason.
		if !wasInOrg && nowInOrg {
			empNo, err := a.contactDB.AllocateEmployeeNo(c)
			if err != nil {
				apiresp.GinError(c, errs.WrapMsg(err, "allocate employee number failed"))
				return
			}
			u.EmployeeNo = empNo
			updateFields["employee_no"] = empNo
			if err := a.contactDB.CreateEmployeeRecord(c, &model.OrgEmployeeRecord{
				OrgCode:    orgCode,
				EmployeeNo: empNo,
				UserID:     u.UserID,
				Account:    u.Account,
				Nickname:   u.Nickname,
				JobTitle:   u.JobTitle,
				DeptNames:  deptNames,
				Status:     model.OrgEmployeeStatusActive,
				JoinedAt:   now,
			}); err != nil {
				apiresp.GinError(c, errs.WrapMsg(err, "create employee archive record failed"))
				return
			}
		} else if wasInOrg && nowInOrg {
			if strings.TrimSpace(u.EmployeeNo) == "" {
				empNo, err := a.contactDB.AllocateEmployeeNo(c)
				if err != nil {
					apiresp.GinError(c, errs.WrapMsg(err, "backfill employee number failed"))
					return
				}
				u.EmployeeNo = empNo
				updateFields["employee_no"] = empNo
				if err := a.contactDB.CreateEmployeeRecord(c, &model.OrgEmployeeRecord{
					OrgCode:    orgCode,
					EmployeeNo: empNo,
					UserID:     u.UserID,
					Account:    u.Account,
					Nickname:   u.Nickname,
					JobTitle:   u.JobTitle,
					DeptNames:  deptNames,
					Status:     model.OrgEmployeeStatusActive,
					JoinedAt:   now,
				}); err != nil {
					apiresp.GinError(c, errs.WrapMsg(err, "create backfilled employee archive record failed"))
					return
				}
			} else if deptsChanged || jobTitleChanged {
				_ = a.contactDB.UpdateActiveEmployeeRecord(c, u.UserID, u.Nickname, u.JobTitle, deptNames)
			}
		} else if wasInOrg && !nowInOrg {
			prevDeptNames := make([]string, 0, len(prevDeptsByUser[u.UserID]))
			for _, pid := range prevDeptsByUser[u.UserID] {
				if nm := prevDeptNameByID[pid]; nm != "" {
					prevDeptNames = append(prevDeptNames, nm)
				}
			}
			reason := truncateRunes(req.RemovalReasons[u.UserID], 300)
			modified, err := a.contactDB.MarkEmployeeRecordRemoved(
				c, u.UserID, now, reason, founderUserID, founderAccount, prevDeptNames,
			)
			if err != nil {
				apiresp.GinError(c, errs.WrapMsg(err, "archive removed employee record failed"))
				return
			}
			if modified == 0 {
				// Ensure even a legacy org member without a prior active record gets an archived record.
				empNo := strings.TrimSpace(u.EmployeeNo)
				if empNo == "" {
					if allocated, allocErr := a.contactDB.AllocateEmployeeNo(c); allocErr == nil {
						empNo = allocated
						u.EmployeeNo = empNo
						updateFields["employee_no"] = empNo
					}
				}
				if empNo != "" {
					_ = a.contactDB.CreateEmployeeRecord(c, &model.OrgEmployeeRecord{
						OrgCode:          orgCode,
						EmployeeNo:       empNo,
						UserID:           u.UserID,
						Account:          u.Account,
						Nickname:         u.Nickname,
						JobTitle:         u.JobTitle,
						DeptNames:        prevDeptNames,
						Status:           model.OrgEmployeeStatusRemoved,
						JoinedAt:         now,
						RemovedAt:        now,
						RemovedReason:    reason,
						RemovedByUserID:  founderUserID,
						RemovedByAccount: founderAccount,
					})
				}
			}
		}

		if !deptsChanged && !jobTitleChanged && len(updateFields) == 0 && len(nextDepts) == 0 {
			continue
		}

		newTokens := model.BuildSearchTokens(u, deptNames...)
		if len(updateFields) > 0 || !stringSlicesEqual(u.SearchTokens, newTokens) {
			u.SearchTokens = newTokens
			updateFields["search_tokens"] = newTokens
			if err := a.userStorageHandler.UpdateProfile(c, u.UserID, u.Account, updateFields); err != nil {
				apiresp.GinError(c, errs.WrapMsg(err, "update user org metadata failed"))
				return
			}
			updatedUsersCount++
		}
	}

	// Broadcast real-time org.tree_updated event over Redis Pub/Sub to all online clients!
	notified := false
	if a.eventHub != nil {
		if err := a.eventHub.PublishOrgTreeUpdated(c, appliedVersion); err == nil {
			notified = true
		}
	}

	snapshot, err := a.buildOrgSnapshotData(c)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	snapshot["notified"] = notified
	snapshot["updatedUsersCount"] = updatedUsersCount
	apiresp.GinSuccess(c, snapshot)
}
