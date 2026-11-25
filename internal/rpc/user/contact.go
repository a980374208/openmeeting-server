package user

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/openmeeting-server/pkg/common/servererrs"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/openmeeting-server/pkg/contactrpc"
	"github.com/openimsdk/openmeeting-server/pkg/events"
	"github.com/openimsdk/tools/errs"
)

func (s *userServer) deptNameMap(ctx context.Context) map[string]string {
	depts, _, err := s.contactDB.ListDepartments(ctx)
	if err != nil {
		return nil
	}
	m := make(map[string]string, len(depts))
	for _, d := range depts {
		m[d.DeptID] = d.Name
	}
	return m
}

func toUserProfileDTO(u *model.User, deptNames map[string]string) contactrpc.UserProfileDTO {
	if u == nil {
		return contactrpc.UserProfileDTO{}
	}
	names := make([]string, 0, len(u.PrimaryDeptIDs))
	for _, id := range u.PrimaryDeptIDs {
		if name, ok := deptNames[id]; ok && name != "" {
			names = append(names, name)
		}
	}
	return contactrpc.UserProfileDTO{
		UserID:          u.UserID,
		Account:         u.Account,
		Nickname:        u.Nickname,
		EmployeeNo:      u.EmployeeNo,
		Email:           u.Email,
		Phone:           u.Phone,
		JobTitle:        u.JobTitle,
		Signature:       u.Signature,
		AvatarURL:       u.AvatarURL,
		PrimaryDeptIDs:  append([]string(nil), u.PrimaryDeptIDs...),
		DepartmentNames: names,
		InOrgDirectory:  len(u.PrimaryDeptIDs) > 0,
		Status:          u.Status,
		UpdatedAtMs:     u.UpdatedAt,
	}
}

func toRelationDTO(rel *model.ContactRelation, profile *contactrpc.UserProfileDTO) contactrpc.ContactRelationDTO {
	if rel == nil {
		return contactrpc.ContactRelationDTO{}
	}
	return contactrpc.ContactRelationDTO{
		OwnerUserID:  rel.OwnerUserID,
		TargetUserID: rel.TargetUserID,
		Remark:       rel.Remark,
		IsFavorite:   rel.IsFavorite,
		IsBlocked:    rel.IsBlocked,
		GroupIDs:     append([]string(nil), rel.GroupIDs...),
		SourceType:   rel.SourceType,
		CreatedAtMs:  rel.CreatedAt,
		UpdatedAtMs:  rel.UpdatedAt,
		Profile:      profile,
	}
}

func (s *userServer) GetUserProfile(ctx context.Context, req *contactrpc.GetUserProfileReq) (*contactrpc.GetUserProfileResp, error) {
	targetID := strings.TrimSpace(req.TargetUserID)
	if targetID == "" {
		targetID = strings.TrimSpace(req.OperatorUserID)
	}
	if targetID == "" {
		return nil, errs.ErrArgs.WrapMsg("targetUserID is required")
	}
	users, err := s.userStorageHandler.FindByUserIDs(ctx, []string{targetID})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg("user not found")
	}
	deptNames := s.deptNameMap(ctx)
	resp := &contactrpc.GetUserProfileResp{
		Profile: toUserProfileDTO(users[0], deptNames),
	}
	operatorID := strings.TrimSpace(req.OperatorUserID)
	if operatorID != "" && operatorID == targetID {
		// Only the user themselves can inspect their own historical employee numbers & org removal reasons.
		if recs, err := s.contactDB.ListUserEmployeeRecords(ctx, targetID); err == nil && len(recs) > 0 {
			history := make([]contactrpc.OrgEmployeeRecordDTO, 0, len(recs))
			for _, rec := range recs {
				history = append(history, contactrpc.OrgEmployeeRecordDTO{
					RecordID:      rec.RecordID,
					OrgCode:       rec.OrgCode,
					EmployeeNo:    rec.EmployeeNo,
					JobTitle:      rec.JobTitle,
					DeptNames:     append([]string(nil), rec.DeptNames...),
					Status:        rec.Status,
					JoinedAtMs:    rec.JoinedAt,
					RemovedAtMs:   rec.RemovedAt,
					RemovedReason: rec.RemovedReason,
				})
			}
			resp.OrgHistory = history
		}
	} else if operatorID != "" && operatorID != targetID {
		if rel, err := s.contactDB.GetRelation(ctx, operatorID, targetID); err == nil && rel != nil {
			dto := toRelationDTO(rel, nil)
			resp.Relation = &dto
		}
	}
	return resp, nil
}

func (s *userServer) UpdateUserProfile(ctx context.Context, req *contactrpc.UpdateUserProfileReq) (*contactrpc.UpdateUserProfileResp, error) {
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}
	users, err := s.userStorageHandler.FindByUserIDs(ctx, []string{userID})
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg("user not found")
	}
	u := users[0]
	update := make(map[string]any)
	if req.Nickname != nil {
		nick := strings.TrimSpace(*req.Nickname)
		if nick == "" || len([]rune(nick)) > 64 {
			return nil, errs.ErrArgs.WrapMsg("nickname must be 1..64 characters")
		}
		u.Nickname = nick
		update["nickname"] = nick
	}
	if req.Signature != nil {
		sig := strings.TrimSpace(*req.Signature)
		if len([]rune(sig)) > 128 {
			sig = string([]rune(sig)[:128])
		}
		u.Signature = sig
		update["signature"] = sig
	}
	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		if len([]rune(email)) > 128 {
			return nil, errs.ErrArgs.WrapMsg("email must be at most 128 characters")
		}
		u.Email = email
		update["email"] = email
	}
	if req.Phone != nil {
		phone := strings.TrimSpace(*req.Phone)
		if len([]rune(phone)) > 32 {
			return nil, errs.ErrArgs.WrapMsg("phone must be at most 32 characters")
		}
		u.Phone = phone
		update["phone"] = phone
	}
	deptNames := s.deptNameMap(ctx)
	if len(update) > 0 {
		var extraDeptNames []string
		for _, id := range u.PrimaryDeptIDs {
			if name := deptNames[id]; name != "" {
				extraDeptNames = append(extraDeptNames, name)
			}
		}
		u.SearchTokens = model.BuildSearchTokens(u, extraDeptNames...)
		update["search_tokens"] = u.SearchTokens
		if err := s.userStorageHandler.UpdateProfile(ctx, u.UserID, u.Account, update); err != nil {
			return nil, err
		}
		if req.Nickname != nil && len(u.PrimaryDeptIDs) > 0 {
			_ = s.contactDB.UpdateActiveEmployeeRecord(ctx, u.UserID, u.Nickname, u.JobTitle, extraDeptNames)
		}
	}
	return &contactrpc.UpdateUserProfileResp{
		Profile: toUserProfileDTO(u, deptNames),
	}, nil
}

func (s *userServer) SearchDirectory(ctx context.Context, req *contactrpc.SearchDirectoryReq) (*contactrpc.SearchDirectoryResp, error) {
	keyword := strings.TrimSpace(req.Keyword)
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "all"
	}
	deptNames := s.deptNameMap(ctx)

	// Exact account / employeeNo / email lookup allows discovering external accounts to send contact requests.
	if scope == "exact" {
		if keyword == "" {
			return &contactrpc.SearchDirectoryResp{Users: []contactrpc.UserProfileDTO{}}, nil
		}
		u, err := s.userStorageHandler.TakeByExactIdentifier(ctx, keyword)
		if err != nil {
			if errs.ErrRecordNotFound.Is(err) {
				return &contactrpc.SearchDirectoryResp{Users: []contactrpc.UserProfileDTO{}}, nil
			}
			return nil, err
		}
		return &contactrpc.SearchDirectoryResp{
			Total: 1,
			Users: []contactrpc.UserProfileDTO{toUserProfileDTO(u, deptNames)},
		}, nil
	}

	var restrictIDs []string
	if scope == "contacts" {
		rels, err := s.contactDB.ListRelations(ctx, req.OperatorUserID, 0)
		if err != nil {
			return nil, err
		}
		restrictIDs = make([]string, 0, len(rels))
		for _, r := range rels {
			if !r.IsBlocked {
				restrictIDs = append(restrictIDs, r.TargetUserID)
			}
		}
	} else if (scope == "all" || scope == "org") && strings.TrimSpace(req.OperatorUserID) != "" {
		depts, _, err := s.contactDB.ListDepartments(ctx)
		if err != nil {
			return nil, err
		}
		hasOrg, canViewAll, ownDeptSet, err := s.resolveUserOrgVisibility(ctx, req.OperatorUserID, depts)
		if err != nil {
			return nil, err
		}
		if hasOrg && !canViewAll {
			reqDept := strings.TrimSpace(req.DeptID)
			if reqDept != "" {
				if _, ok := ownDeptSet[reqDept]; !ok {
					return &contactrpc.SearchDirectoryResp{
						Total: 0,
						Users: []contactrpc.UserProfileDTO{},
					}, nil
				}
			}
			allowedSet := map[string]struct{}{strings.TrimSpace(req.OperatorUserID): {}}
			for ownDeptID := range ownDeptSet {
				if mems, _, merr := s.contactDB.ListDepartmentMembers(ctx, ownDeptID, 0, 200); merr == nil {
					for _, m := range mems {
						if m.UserID != "" {
							allowedSet[m.UserID] = struct{}{}
						}
					}
				}
			}
			if scope == "all" {
				if rels, rerr := s.contactDB.ListRelations(ctx, req.OperatorUserID, 0); rerr == nil {
					for _, r := range rels {
						if !r.IsBlocked && r.TargetUserID != "" {
							allowedSet[r.TargetUserID] = struct{}{}
						}
					}
				}
			}
			restrictIDs = make([]string, 0, len(allowedSet))
			for uid := range allowedSet {
				restrictIDs = append(restrictIDs, uid)
			}
		}
	}

	users, total, err := s.userStorageHandler.SearchByPrefix(ctx, keyword, strings.TrimSpace(req.DeptID), restrictIDs, req.Offset, req.Limit)
	if err != nil {
		return nil, err
	}
	out := make([]contactrpc.UserProfileDTO, 0, len(users))
	seen := make(map[string]struct{}, len(users))
	for _, u := range users {
		seen[u.UserID] = struct{}{}
		out = append(out, toUserProfileDTO(u, deptNames))
	}
	// If scope=="all" and keyword looks like an exact identifier not yet matched, append exact lookup.
	if scope == "all" && keyword != "" && req.DeptID == "" && req.Offset == 0 {
		if exact, err := s.userStorageHandler.TakeByExactIdentifier(ctx, keyword); err == nil && exact != nil {
			if _, ok := seen[exact.UserID]; !ok {
				out = append(out, toUserProfileDTO(exact, deptNames))
				total++
			}
		}
	}
	return &contactrpc.SearchDirectoryResp{
		Total: total,
		Users: out,
	}, nil
}

func (s *userServer) resolveUserOrgVisibility(
	ctx context.Context,
	userID string,
	depts []*model.OrgDepartment,
) (hasOrg bool, canViewAll bool, ownDeptSet map[string]struct{}, err error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || len(depts) == 0 {
		return false, false, nil, nil
	}
	members, err := s.contactDB.ListUserDepartments(ctx, userID)
	if err != nil {
		return false, false, nil, err
	}
	if len(members) == 0 {
		return false, false, nil, nil
	}
	orgDefault, err := s.contactDB.GetOrgMemberVisibility(ctx)
	if err != nil {
		return false, false, nil, err
	}
	orgDefault = model.NormalizeOrgMemberVisibility(orgDefault)

	deptByID := make(map[string]*model.OrgDepartment, len(depts))
	for _, d := range depts {
		deptByID[d.DeptID] = d
	}
	ownDeptSet = make(map[string]struct{}, len(members))
	for _, m := range members {
		d, ok := deptByID[m.DeptID]
		if !ok {
			continue
		}
		ownDeptSet[d.DeptID] = struct{}{}
		if d.EffectiveMemberVisibility(orgDefault) == model.OrgMemberVisibilityAll {
			canViewAll = true
		}
	}
	return len(ownDeptSet) > 0, canViewAll, ownDeptSet, nil
}

func (s *userServer) GetOrgTree(ctx context.Context, req *contactrpc.GetOrgTreeReq) (*contactrpc.GetOrgTreeResp, error) {
	depts, version, err := s.contactDB.ListDepartments(ctx)
	if err != nil {
		return nil, err
	}
	if len(depts) == 0 {
		return &contactrpc.GetOrgTreeResp{
			TreeVersion:      version,
			NotModified:      false,
			MemberVisibility: model.OrgMemberVisibilityAll,
			Departments:      []contactrpc.OrgDepartmentDTO{},
		}, nil
	}
	effectiveVisibility := model.OrgMemberVisibilityAll
	// Only users who belong to the organization directory receive the org tree,
	// and all org members always see the full org hierarchy regardless of member visibility policy.
	if strings.TrimSpace(req.OperatorUserID) != "" {
		hasOrg, canViewAll, _, err := s.resolveUserOrgVisibility(ctx, req.OperatorUserID, depts)
		if err != nil {
			return nil, err
		}
		if !hasOrg {
			return &contactrpc.GetOrgTreeResp{
				TreeVersion: version,
				NotModified: false,
				Departments: []contactrpc.OrgDepartmentDTO{},
			}, nil
		}
		if !canViewAll {
			effectiveVisibility = model.OrgMemberVisibilityOwnDept
		}
	}
	if req.ClientVersion > 0 && req.ClientVersion == version {
		return &contactrpc.GetOrgTreeResp{
			TreeVersion:      version,
			NotModified:      true,
			MemberVisibility: effectiveVisibility,
			Departments:      []contactrpc.OrgDepartmentDTO{},
		}, nil
	}
	out := make([]contactrpc.OrgDepartmentDTO, 0, len(depts))
	for _, d := range depts {
		out = append(out, contactrpc.OrgDepartmentDTO{
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
	return &contactrpc.GetOrgTreeResp{
		TreeVersion:      version,
		NotModified:      false,
		MemberVisibility: effectiveVisibility,
		Departments:      out,
	}, nil
}

func (s *userServer) GetDeptMembers(ctx context.Context, req *contactrpc.GetDeptMembersReq) (*contactrpc.GetDeptMembersResp, error) {
	deptID := strings.TrimSpace(req.DeptID)
	if deptID == "" {
		return nil, errs.ErrArgs.WrapMsg("deptID is required")
	}
	if strings.TrimSpace(req.OperatorUserID) != "" {
		depts, _, err := s.contactDB.ListDepartments(ctx)
		if err != nil {
			return nil, err
		}
		hasOrg, canViewAll, ownDeptSet, err := s.resolveUserOrgVisibility(ctx, req.OperatorUserID, depts)
		if err != nil {
			return nil, err
		}
		if !hasOrg {
			return &contactrpc.GetDeptMembersResp{
				DeptID:  deptID,
				Total:   0,
				Members: []contactrpc.DeptMemberDTO{},
			}, nil
		}
		if !canViewAll {
			if _, ok := ownDeptSet[deptID]; !ok {
				return &contactrpc.GetDeptMembersResp{
					DeptID:     deptID,
					Total:      0,
					Members:    []contactrpc.DeptMemberDTO{},
					Restricted: true,
				}, nil
			}
		}
	}
	members, total, err := s.contactDB.ListDepartmentMembers(ctx, deptID, req.Offset, req.Limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}
	users, err := s.userStorageHandler.FindByUserIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	userMap := make(map[string]*model.User, len(users))
	for _, u := range users {
		userMap[u.UserID] = u
	}
	deptNames := s.deptNameMap(ctx)
	out := make([]contactrpc.DeptMemberDTO, 0, len(members))
	for _, m := range members {
		u := userMap[m.UserID]
		if u == nil || u.Status == model.UserStatusDisabled {
			continue
		}
		out = append(out, contactrpc.DeptMemberDTO{
			DeptID:    m.DeptID,
			IsLeader:  m.IsLeader,
			SortOrder: m.SortOrder,
			Profile:   toUserProfileDTO(u, deptNames),
		})
	}
	return &contactrpc.GetDeptMembersResp{
		DeptID:  deptID,
		Total:   total,
		Members: out,
	}, nil
}

func (s *userServer) SyncContactSnapshot(ctx context.Context, req *contactrpc.SyncContactSnapshotReq) (*contactrpc.SyncContactSnapshotResp, error) {
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}
	rels, err := s.contactDB.ListRelations(ctx, userID, req.UpdatedAfterMs)
	if err != nil {
		return nil, err
	}
	groups, err := s.contactDB.ListGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	reqs, err := s.contactDB.ListRequests(ctx, userID, 100)
	if err != nil {
		return nil, err
	}
	var pendingCount int64
	for _, r := range reqs {
		if r.ToUserID == userID && r.Status == model.ContactRequestPending {
			pendingCount++
		}
	}

	targetIDs := make([]string, 0, len(rels))
	for _, r := range rels {
		targetIDs = append(targetIDs, r.TargetUserID)
	}
	users, err := s.userStorageHandler.FindByUserIDs(ctx, targetIDs)
	if err != nil {
		return nil, err
	}
	userMap := make(map[string]*model.User, len(users))
	for _, u := range users {
		userMap[u.UserID] = u
	}
	deptNames := s.deptNameMap(ctx)
	contactDTOs := make([]contactrpc.ContactRelationDTO, 0, len(rels))
	for _, r := range rels {
		var prof *contactrpc.UserProfileDTO
		if u := userMap[r.TargetUserID]; u != nil {
			p := toUserProfileDTO(u, deptNames)
			prof = &p
		}
		contactDTOs = append(contactDTOs, toRelationDTO(r, prof))
	}
	groupDTOs := make([]contactrpc.ContactGroupDTO, 0, len(groups))
	for _, g := range groups {
		groupDTOs = append(groupDTOs, contactrpc.ContactGroupDTO{
			GroupID:     g.GroupID,
			Name:        g.Name,
			SortOrder:   g.SortOrder,
			UpdatedAtMs: g.UpdatedAt,
		})
	}
	return &contactrpc.SyncContactSnapshotResp{
		ServerTimeMs: time.Now().UnixMilli(),
		Contacts:     contactDTOs,
		Groups:       groupDTOs,
		PendingCount: pendingCount,
	}, nil
}

func (s *userServer) UpsertContactRelation(ctx context.Context, req *contactrpc.UpsertContactRelationReq) (*contactrpc.UpsertContactRelationResp, error) {
	ownerID := strings.TrimSpace(req.OwnerUserID)
	targetID := strings.TrimSpace(req.TargetUserID)
	if ownerID == "" || targetID == "" || ownerID == targetID {
		return nil, errs.ErrArgs.WrapMsg("invalid ownerUserID or targetUserID")
	}
	targetUsers, err := s.userStorageHandler.FindByUserIDs(ctx, []string{targetID})
	if err != nil {
		return nil, err
	}
	if len(targetUsers) == 0 {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg("target user not found")
	}

	existing, err := s.contactDB.GetRelation(ctx, ownerID, targetID)
	if err != nil && !errs.ErrRecordNotFound.Is(err) {
		return nil, err
	}
	rel := existing
	if rel == nil {
		inSameOrg, _ := s.contactDB.AreInSameOrgDirectory(ctx, ownerID, targetID)
		isBlocking := req.IsBlocked != nil && *req.IsBlocked
		// External accounts require ContactRequest approval unless merely blocking them.
		if !inSameOrg && !isBlocking {
			return nil, servererrs.ErrContactNotFound.WrapMsg("external user must be added via contact request first")
		}
		source := model.ContactSourceOrgColleague
		if !inSameOrg {
			source = model.ContactSourcePersonalAdd
		}
		rel = &model.ContactRelation{
			OwnerUserID:  ownerID,
			TargetUserID: targetID,
			SourceType:   source,
			GroupIDs:     []string{},
		}
	}
	if req.Remark != nil {
		remark := strings.TrimSpace(*req.Remark)
		if len([]rune(remark)) > 64 {
			remark = string([]rune(remark)[:64])
		}
		rel.Remark = remark
	}
	if req.IsFavorite != nil {
		rel.IsFavorite = *req.IsFavorite
	}
	if req.IsBlocked != nil {
		rel.IsBlocked = *req.IsBlocked
	}
	if req.HasGroupIDs {
		rel.GroupIDs = append([]string(nil), req.GroupIDs...)
	}
	if err := s.contactDB.UpsertRelation(ctx, rel); err != nil {
		return nil, err
	}
	deptNames := s.deptNameMap(ctx)
	prof := toUserProfileDTO(targetUsers[0], deptNames)
	dto := toRelationDTO(rel, &prof)
	if s.eventHub != nil {
		_, _ = s.eventHub.PublishDurableEvent(ctx, ownerID, events.TypeContactRelationUpdated, dto)
	}
	return &contactrpc.UpsertContactRelationResp{Relation: dto}, nil
}

func (s *userServer) RemoveContactRelation(ctx context.Context, req *contactrpc.RemoveContactRelationReq) (*contactrpc.RemoveContactRelationResp, error) {
	ownerID := strings.TrimSpace(req.OwnerUserID)
	targetID := strings.TrimSpace(req.TargetUserID)
	if ownerID == "" || targetID == "" {
		return nil, errs.ErrArgs.WrapMsg("ownerUserID and targetUserID are required")
	}
	if err := s.contactDB.RemoveRelation(ctx, ownerID, targetID); err != nil {
		return nil, err
	}
	if s.eventHub != nil {
		_, _ = s.eventHub.PublishDurableEvent(ctx, ownerID, events.TypeContactRelationUpdated, map[string]any{
			"ownerUserID":  ownerID,
			"targetUserID": targetID,
			"removed":      true,
		})
	}
	return &contactrpc.RemoveContactRelationResp{Removed: true}, nil
}

func (s *userServer) ManageContactGroup(ctx context.Context, req *contactrpc.ManageContactGroupReq) (*contactrpc.ManageContactGroupResp, error) {
	ownerID := strings.TrimSpace(req.OwnerUserID)
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if ownerID == "" {
		return nil, errs.ErrArgs.WrapMsg("ownerUserID is required")
	}
	var affected *contactrpc.ContactGroupDTO
	switch action {
	case "create":
		name := strings.TrimSpace(req.Name)
		if name == "" || len([]rune(name)) > 32 {
			return nil, errs.ErrArgs.WrapMsg("group name must be 1..32 characters")
		}
		g := &model.ContactGroup{
			GroupID:     "grp_" + uuid.New().String()[:12],
			OwnerUserID: ownerID,
			Name:        name,
			SortOrder:   req.SortOrder,
		}
		if err := s.contactDB.UpsertGroup(ctx, g); err != nil {
			return nil, err
		}
		affected = &contactrpc.ContactGroupDTO{GroupID: g.GroupID, Name: g.Name, SortOrder: g.SortOrder, UpdatedAtMs: g.UpdatedAt}
	case "rename", "reorder":
		groupID := strings.TrimSpace(req.GroupID)
		name := strings.TrimSpace(req.Name)
		if groupID == "" || name == "" {
			return nil, errs.ErrArgs.WrapMsg("groupID and name are required")
		}
		g := &model.ContactGroup{
			GroupID:     groupID,
			OwnerUserID: ownerID,
			Name:        name,
			SortOrder:   req.SortOrder,
		}
		if err := s.contactDB.UpsertGroup(ctx, g); err != nil {
			return nil, err
		}
		affected = &contactrpc.ContactGroupDTO{GroupID: g.GroupID, Name: g.Name, SortOrder: g.SortOrder, UpdatedAtMs: g.UpdatedAt}
	case "delete":
		groupID := strings.TrimSpace(req.GroupID)
		if groupID == "" {
			return nil, errs.ErrArgs.WrapMsg("groupID is required")
		}
		if err := s.contactDB.DeleteGroup(ctx, ownerID, groupID); err != nil {
			return nil, err
		}
	default:
		return nil, errs.ErrArgs.WrapMsg("unsupported action: " + action)
	}

	groups, err := s.contactDB.ListGroups(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	out := make([]contactrpc.ContactGroupDTO, 0, len(groups))
	for _, g := range groups {
		out = append(out, contactrpc.ContactGroupDTO{
			GroupID:     g.GroupID,
			Name:        g.Name,
			SortOrder:   g.SortOrder,
			UpdatedAtMs: g.UpdatedAt,
		})
	}
	return &contactrpc.ManageContactGroupResp{Group: affected, Groups: out}, nil
}

func (s *userServer) SendContactRequest(ctx context.Context, req *contactrpc.SendContactRequestReq) (*contactrpc.SendContactRequestResp, error) {
	fromID := strings.TrimSpace(req.FromUserID)
	toID := strings.TrimSpace(req.ToUserID)
	if fromID == "" || toID == "" || fromID == toID {
		return nil, errs.ErrArgs.WrapMsg("invalid fromUserID or toUserID")
	}
	users, err := s.userStorageHandler.FindByUserIDs(ctx, []string{fromID, toID})
	if err != nil {
		return nil, err
	}
	userMap := make(map[string]*model.User, len(users))
	for _, u := range users {
		userMap[u.UserID] = u
	}
	if userMap[toID] == nil {
		return nil, servererrs.ErrUserIDNotFound.WrapMsg("target user not found")
	}
	blocked, err := s.contactDB.IsBlocked(ctx, toID, fromID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, servererrs.ErrContactBlocked.WrapMsg("cannot send contact request")
	}
	deptNames := s.deptNameMap(ctx)
	// Same organization colleagues do not require approval; auto-add to sender's frequent contacts.
	if sameOrg, _ := s.contactDB.AreInSameOrgDirectory(ctx, fromID, toID); sameOrg {
		rel := &model.ContactRelation{
			OwnerUserID:  fromID,
			TargetUserID: toID,
			SourceType:   model.ContactSourceOrgColleague,
			GroupIDs:     []string{},
		}
		if err := s.contactDB.UpsertRelation(ctx, rel); err != nil {
			return nil, err
		}
		prof := toUserProfileDTO(userMap[toID], deptNames)
		dto := toRelationDTO(rel, &prof)
		if s.eventHub != nil {
			_, _ = s.eventHub.PublishDurableEvent(ctx, fromID, events.TypeContactRelationUpdated, dto)
		}
		return &contactrpc.SendContactRequestResp{
			AutoAccepted: true,
			Relation:     &dto,
		}, nil
	}

	// Deduplicate pending request
	if pending, err := s.contactDB.FindPendingRequest(ctx, fromID, toID); err == nil && pending != nil {
		dto := s.buildRequestDTO(pending, userMap, deptNames)
		return &contactrpc.SendContactRequestResp{Request: &dto}, nil
	}

	greeting := strings.TrimSpace(req.Greeting)
	if len([]rune(greeting)) > 120 {
		greeting = string([]rune(greeting)[:120])
	}
	record := &model.ContactRequest{
		RequestID:  "creq_" + uuid.New().String()[:12],
		FromUserID: fromID,
		ToUserID:   toID,
		Greeting:   greeting,
		Status:     model.ContactRequestPending,
		CreatedAt:  time.Now().UnixMilli(),
	}
	if err := s.contactDB.CreateRequest(ctx, record); err != nil {
		return nil, err
	}
	dto := s.buildRequestDTO(record, userMap, deptNames)
	if s.eventHub != nil {
		_, _ = s.eventHub.PublishDurableEvent(ctx, toID, events.TypeContactRequestReceived, dto)
	}
	return &contactrpc.SendContactRequestResp{Request: &dto}, nil
}

func (s *userServer) HandleContactRequest(ctx context.Context, req *contactrpc.HandleContactRequestReq) (*contactrpc.HandleContactRequestResp, error) {
	opID := strings.TrimSpace(req.OperatorUserID)
	reqID := strings.TrimSpace(req.RequestID)
	if opID == "" || reqID == "" {
		return nil, errs.ErrArgs.WrapMsg("operatorUserID and requestID are required")
	}
	record, err := s.contactDB.GetRequest(ctx, reqID)
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return nil, servererrs.ErrContactRequestNotFound.WrapMsg("contact request not found")
		}
		return nil, err
	}
	if record.ToUserID != opID {
		return nil, servererrs.ErrMeetingAuthCheck.WrapMsg("not authorized to handle this contact request")
	}
	if record.Status != model.ContactRequestPending {
		return nil, servererrs.ErrContactRequestAlreadyHandled.WrapMsg("contact request already handled")
	}
	newStatus := model.ContactRequestRejected
	if req.Accept {
		newStatus = model.ContactRequestAccepted
	}
	now := time.Now().UnixMilli()
	updated, err := s.contactDB.UpdateRequestStatus(ctx, reqID, model.ContactRequestPending, newStatus, now)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, servererrs.ErrContactRequestAlreadyHandled.WrapMsg("contact request already handled")
	}
	record.Status = newStatus
	record.HandledAt = now

	users, _ := s.userStorageHandler.FindByUserIDs(ctx, []string{record.FromUserID, record.ToUserID})
	userMap := make(map[string]*model.User, len(users))
	for _, u := range users {
		userMap[u.UserID] = u
	}
	deptNames := s.deptNameMap(ctx)
	reqDTO := s.buildRequestDTO(record, userMap, deptNames)

	var relDTO *contactrpc.ContactRelationDTO
	if req.Accept {
		// Create mutual personal contact relations
		relTo := &model.ContactRelation{
			OwnerUserID:  record.ToUserID,
			TargetUserID: record.FromUserID,
			SourceType:   model.ContactSourcePersonalAdd,
			GroupIDs:     []string{},
		}
		relFrom := &model.ContactRelation{
			OwnerUserID:  record.FromUserID,
			TargetUserID: record.ToUserID,
			SourceType:   model.ContactSourcePersonalAdd,
			GroupIDs:     []string{},
		}
		_ = s.contactDB.UpsertRelation(ctx, relTo)
		_ = s.contactDB.UpsertRelation(ctx, relFrom)

		var fromProf *contactrpc.UserProfileDTO
		if u := userMap[record.FromUserID]; u != nil {
			p := toUserProfileDTO(u, deptNames)
			fromProf = &p
		}
		d := toRelationDTO(relTo, fromProf)
		relDTO = &d

		if s.eventHub != nil {
			var toProf *contactrpc.UserProfileDTO
			if u := userMap[record.ToUserID]; u != nil {
				p := toUserProfileDTO(u, deptNames)
				toProf = &p
			}
			_, _ = s.eventHub.PublishDurableEvent(ctx, record.FromUserID, events.TypeContactRelationUpdated, toRelationDTO(relFrom, toProf))
			_, _ = s.eventHub.PublishDurableEvent(ctx, record.ToUserID, events.TypeContactRelationUpdated, d)
		}
	}
	if s.eventHub != nil {
		_, _ = s.eventHub.PublishDurableEvent(ctx, record.FromUserID, events.TypeContactRequestHandled, reqDTO)
		_, _ = s.eventHub.PublishDurableEvent(ctx, record.ToUserID, events.TypeContactRequestHandled, reqDTO)
	}
	return &contactrpc.HandleContactRequestResp{
		Request:  reqDTO,
		Relation: relDTO,
	}, nil
}

func (s *userServer) ListContactRequests(ctx context.Context, req *contactrpc.ListContactRequestsReq) (*contactrpc.ListContactRequestsResp, error) {
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		return nil, errs.ErrArgs.WrapMsg("userID is required")
	}
	records, err := s.contactDB.ListRequests(ctx, userID, req.Limit)
	if err != nil {
		return nil, err
	}
	idSet := make(map[string]struct{}, len(records)*2)
	var ids []string
	for _, r := range records {
		for _, id := range []string{r.FromUserID, r.ToUserID} {
			if _, ok := idSet[id]; !ok {
				idSet[id] = struct{}{}
				ids = append(ids, id)
			}
		}
	}
	users, err := s.userStorageHandler.FindByUserIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	userMap := make(map[string]*model.User, len(users))
	for _, u := range users {
		userMap[u.UserID] = u
	}
	deptNames := s.deptNameMap(ctx)
	out := make([]contactrpc.ContactRequestDTO, 0, len(records))
	var pendingCount int64
	for _, r := range records {
		if r.ToUserID == userID && r.Status == model.ContactRequestPending {
			pendingCount++
		}
		out = append(out, s.buildRequestDTO(r, userMap, deptNames))
	}
	return &contactrpc.ListContactRequestsResp{
		Requests:     out,
		PendingCount: pendingCount,
	}, nil
}

func (s *userServer) CheckBlocked(ctx context.Context, req *contactrpc.CheckBlockedReq) (*contactrpc.CheckBlockedResp, error) {
	blocked, err := s.contactDB.IsBlocked(ctx, req.OwnerUserID, req.TargetUserID)
	if err != nil {
		return nil, err
	}
	return &contactrpc.CheckBlockedResp{Blocked: blocked}, nil
}

func (s *userServer) buildRequestDTO(r *model.ContactRequest, userMap map[string]*model.User, deptNames map[string]string) contactrpc.ContactRequestDTO {
	dto := contactrpc.ContactRequestDTO{
		RequestID:   r.RequestID,
		FromUserID:  r.FromUserID,
		ToUserID:    r.ToUserID,
		Greeting:    r.Greeting,
		Status:      r.Status,
		CreatedAtMs: r.CreatedAt,
		HandledAtMs: r.HandledAt,
	}
	if u := userMap[r.FromUserID]; u != nil {
		p := toUserProfileDTO(u, deptNames)
		dto.FromProfile = &p
	}
	if u := userMap[r.ToUserID]; u != nil {
		p := toUserProfileDTO(u, deptNames)
		dto.ToProfile = &p
	}
	return dto
}
