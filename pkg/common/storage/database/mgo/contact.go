package mgo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/database"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type ContactMgo struct {
	deptColl      *mongo.Collection
	memberColl    *mongo.Collection
	metaColl      *mongo.Collection
	empRecordColl *mongo.Collection
	relColl       *mongo.Collection
	groupColl     *mongo.Collection
	requestColl   *mongo.Collection
}

type orgMetaDoc struct {
	ID               string `bson:"_id"`
	Version          int64  `bson:"version"`
	Seq              int64  `bson:"seq,omitempty"`
	Code             string `bson:"code,omitempty"`
	MemberVisibility string `bson:"member_visibility,omitempty"`
	UpdatedAt        int64  `bson:"updated_at"`
}

const (
	orgTreeMetaDocID     = "org_tree_meta"
	orgSettingsMetaDocID = "org_settings_meta"
	orgCodeMetaDocID     = "org_code_meta"
	orgCodeSeqDocID      = "org_code_seq"
	deptCodeSeqDocID     = "dept_code_seq"
	empNoSeqDocID        = "emp_no_seq"
)

func NewContactMongo(db *mongo.Database) (database.Contact, error) {
	ctx := context.Background()
	deptColl := db.Collection("org_department")
	if _, err := deptColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "dept_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "parent_id", Value: 1}, {Key: "sort_order", Value: 1}}},
		{Keys: bson.D{{Key: "ancestor_path", Value: 1}}},
	}); err != nil {
		return nil, errs.Wrap(err)
	}

	memberColl := db.Collection("user_department_member")
	if _, err := memberColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "dept_id", Value: 1}, {Key: "sort_order", Value: 1}, {Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "dept_id", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		return nil, errs.Wrap(err)
	}

	relColl := db.Collection("contact_relation")
	if _, err := relColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "owner_user_id", Value: 1}, {Key: "target_user_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "owner_user_id", Value: 1}, {Key: "is_blocked", Value: 1}, {Key: "is_favorite", Value: 1}}},
		{Keys: bson.D{{Key: "owner_user_id", Value: 1}, {Key: "updated_at", Value: 1}}},
	}); err != nil {
		return nil, errs.Wrap(err)
	}

	groupColl := db.Collection("contact_group")
	if _, err := groupColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "group_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "owner_user_id", Value: 1}, {Key: "sort_order", Value: 1}}},
	}); err != nil {
		return nil, errs.Wrap(err)
	}

	requestColl := db.Collection("contact_request")
	if _, err := requestColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "request_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "to_user_id", Value: 1}, {Key: "status", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "from_user_id", Value: 1}, {Key: "to_user_id", Value: 1}, {Key: "status", Value: 1}}},
	}); err != nil {
		return nil, errs.Wrap(err)
	}

	empRecordColl := db.Collection("org_employee_record")
	if _, err := empRecordColl.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "record_id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "employee_no", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "joined_at", Value: -1}}},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "updated_at", Value: -1}}},
	}); err != nil {
		return nil, errs.Wrap(err)
	}

	return &ContactMgo{
		deptColl:      deptColl,
		memberColl:    memberColl,
		metaColl:      db.Collection("org_meta"),
		empRecordColl: empRecordColl,
		relColl:       relColl,
		groupColl:     groupColl,
		requestColl:   requestColl,
	}, nil
}

func deterministicDeptID(parentID, name string) string {
	sum := sha256.Sum256([]byte(parentID + "\x00" + strings.TrimSpace(name)))
	return "dept_" + hex.EncodeToString(sum[:8])
}

func (c *ContactMgo) getMetaVersion(ctx context.Context) int64 {
	if c.metaColl == nil {
		return 0
	}
	var doc orgMetaDoc
	if err := c.metaColl.FindOne(ctx, bson.M{"_id": orgTreeMetaDocID}).Decode(&doc); err != nil {
		return 0
	}
	return doc.Version
}

func (c *ContactMgo) BumpOrgTreeVersion(ctx context.Context, minVersion int64) (int64, error) {
	now := time.Now().UnixMilli()
	prev := c.getMetaVersion(ctx)
	next := now
	if minVersion > next {
		next = minVersion
	}
	if prev >= next {
		next = prev + 1
	}
	if c.metaColl != nil {
		_, err := c.metaColl.UpdateOne(ctx,
			bson.M{"_id": orgTreeMetaDocID},
			bson.M{"$set": orgMetaDoc{
				ID:        orgTreeMetaDocID,
				Version:   next,
				UpdatedAt: now,
			}},
			options.Update().SetUpsert(true),
		)
		if err != nil {
			return 0, errs.Wrap(err)
		}
	}
	return next, nil
}

func (c *ContactMgo) ListDepartments(ctx context.Context) ([]*model.OrgDepartment, int64, error) {
	opts := options.Find().SetSort(bson.D{{Key: "parent_id", Value: 1}, {Key: "sort_order", Value: 1}, {Key: "name", Value: 1}})
	depts, err := mongoutil.Find[*model.OrgDepartment](ctx, c.deptColl, bson.M{}, opts)
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	maxVersion := c.getMetaVersion(ctx)
	for _, d := range depts {
		if d.Version > maxVersion {
			maxVersion = d.Version
		}
		if d.UpdatedAt > maxVersion {
			maxVersion = d.UpdatedAt
		}
	}
	return depts, maxVersion, nil
}

func (c *ContactMgo) GetDepartment(ctx context.Context, deptID string) (*model.OrgDepartment, error) {
	dept, err := mongoutil.FindOne[*model.OrgDepartment](ctx, c.deptColl, bson.M{"dept_id": deptID})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errs.ErrRecordNotFound.WrapMsg("department not found")
		}
		return nil, errs.Wrap(err)
	}
	return dept, nil
}

func (c *ContactMgo) UpsertDepartment(ctx context.Context, dept *model.OrgDepartment) error {
	now := time.Now().UnixMilli()
	if dept.UpdatedAt == 0 {
		dept.UpdatedAt = now
	}
	if dept.Version == 0 {
		dept.Version = now
	}
	_, err := c.deptColl.UpdateOne(ctx,
		bson.M{"dept_id": dept.DeptID},
		bson.M{"$set": dept},
		options.Update().SetUpsert(true),
	)
	return errs.Wrap(err)
}

func (c *ContactMgo) EnsureDepartmentPath(ctx context.Context, slashPath string) ([]*model.OrgDepartment, error) {
	rawSegments := strings.Split(strings.TrimSpace(slashPath), "/")
	var segments []string
	for _, s := range rawSegments {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			segments = append(segments, trimmed)
		}
	}
	if len(segments) == 0 {
		return nil, nil
	}
	now := time.Now().UnixMilli()
	parentID := ""
	ancestorPath := "/"
	result := make([]*model.OrgDepartment, 0, len(segments))
	for _, name := range segments {
		deptID := deterministicDeptID(parentID, name)
		deptCode := ""
		if existing, err := c.GetDepartment(ctx, deptID); err == nil && existing != nil && existing.DeptCode != "" {
			deptCode = existing.DeptCode
		} else {
			code, err := c.AllocateDeptCode(ctx)
			if err != nil {
				return nil, err
			}
			deptCode = code
		}
		dept := &model.OrgDepartment{
			DeptID:       deptID,
			DeptCode:     deptCode,
			ParentID:     parentID,
			Name:         name,
			AncestorPath: ancestorPath,
			Version:      now,
			UpdatedAt:    now,
		}
		if err := c.UpsertDepartment(ctx, dept); err != nil {
			return nil, err
		}
		result = append(result, dept)
		parentID = deptID
		ancestorPath = ancestorPath + deptID + "/"
	}
	return result, nil
}

func (c *ContactMgo) SetUserDepartments(ctx context.Context, userID string, deptIDs []string, isLeader bool) error {
	now := time.Now().UnixMilli()
	seen := make(map[string]struct{}, len(deptIDs))
	cleaned := make([]string, 0, len(deptIDs))
	for _, id := range deptIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			cleaned = append(cleaned, id)
		}
	}
	if _, err := c.memberColl.DeleteMany(ctx, bson.M{"user_id": userID, "dept_id": bson.M{"$nin": cleaned}}); err != nil {
		return errs.Wrap(err)
	}
	for i, deptID := range cleaned {
		doc := &model.UserDepartmentMember{
			DeptID:    deptID,
			UserID:    userID,
			IsLeader:  isLeader,
			SortOrder: int32(i),
			UpdatedAt: now,
		}
		if _, err := c.memberColl.UpdateOne(ctx,
			bson.M{"user_id": userID, "dept_id": deptID},
			bson.M{"$set": doc},
			options.Update().SetUpsert(true),
		); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

func (c *ContactMgo) ListDepartmentMembers(ctx context.Context, deptID string, offset, limit int64) ([]*model.UserDepartmentMember, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	filter := bson.M{"dept_id": deptID}
	total, err := c.memberColl.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "is_leader", Value: -1}, {Key: "sort_order", Value: 1}, {Key: "user_id", Value: 1}}).
		SetSkip(offset).
		SetLimit(limit)
	members, err := mongoutil.Find[*model.UserDepartmentMember](ctx, c.memberColl, filter, opts)
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return members, total, nil
}

func (c *ContactMgo) ListUserDepartments(ctx context.Context, userID string) ([]*model.UserDepartmentMember, error) {
	members, err := mongoutil.Find[*model.UserDepartmentMember](ctx, c.memberColl, bson.M{"user_id": userID})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return members, nil
}

func (c *ContactMgo) ListAllDepartmentMembers(ctx context.Context) ([]*model.UserDepartmentMember, error) {
	opts := options.Find().SetSort(bson.D{{Key: "dept_id", Value: 1}, {Key: "is_leader", Value: -1}, {Key: "sort_order", Value: 1}, {Key: "user_id", Value: 1}})
	members, err := mongoutil.Find[*model.UserDepartmentMember](ctx, c.memberColl, bson.M{}, opts)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return members, nil
}

func (c *ContactMgo) ReplaceOrgSnapshot(ctx context.Context, depts []*model.OrgDepartment, members []*model.UserDepartmentMember, minVersion int64) (int64, error) {
	now := time.Now().UnixMilli()
	prev := c.getMetaVersion(ctx)
	version := now
	if minVersion > version {
		version = minVersion
	}
	if prev >= version {
		version = prev + 1
	}

	keepDeptIDs := make([]string, 0, len(depts))
	for _, d := range depts {
		d.Version = version
		d.UpdatedAt = version
		keepDeptIDs = append(keepDeptIDs, d.DeptID)
	}

	if _, err := c.deptColl.DeleteMany(ctx, bson.M{"dept_id": bson.M{"$nin": keepDeptIDs}}); err != nil {
		return 0, errs.Wrap(err)
	}
	for _, d := range depts {
		if _, err := c.deptColl.UpdateOne(ctx,
			bson.M{"dept_id": d.DeptID},
			bson.M{"$set": d},
			options.Update().SetUpsert(true),
		); err != nil {
			return 0, errs.Wrap(err)
		}
	}

	if _, err := c.memberColl.DeleteMany(ctx, bson.M{}); err != nil {
		return 0, errs.Wrap(err)
	}
	if len(members) > 0 {
		docs := make([]any, 0, len(members))
		for _, m := range members {
			m.UpdatedAt = version
			docs = append(docs, m)
		}
		if _, err := c.memberColl.InsertMany(ctx, docs); err != nil {
			return 0, errs.Wrap(err)
		}
	}

	if c.metaColl != nil {
		if _, err := c.metaColl.UpdateOne(ctx,
			bson.M{"_id": orgTreeMetaDocID},
			bson.M{"$set": orgMetaDoc{
				ID:        orgTreeMetaDocID,
				Version:   version,
				UpdatedAt: version,
			}},
			options.Update().SetUpsert(true),
		); err != nil {
			return 0, errs.Wrap(err)
		}
	}
	return version, nil
}

func (c *ContactMgo) GetOrgMemberVisibility(ctx context.Context) (string, error) {
	if c.metaColl == nil {
		return model.OrgMemberVisibilityAll, nil
	}
	var doc orgMetaDoc
	if err := c.metaColl.FindOne(ctx, bson.M{"_id": orgSettingsMetaDocID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return model.OrgMemberVisibilityAll, nil
		}
		return model.OrgMemberVisibilityAll, errs.Wrap(err)
	}
	return model.NormalizeOrgMemberVisibility(doc.MemberVisibility), nil
}

func (c *ContactMgo) SetOrgMemberVisibility(ctx context.Context, visibility string) error {
	if c.metaColl == nil {
		return nil
	}
	normalized := model.NormalizeOrgMemberVisibility(visibility)
	now := time.Now().UnixMilli()
	_, err := c.metaColl.UpdateOne(
		ctx,
		bson.M{"_id": orgSettingsMetaDocID},
		bson.M{"$set": orgMetaDoc{
			ID:               orgSettingsMetaDocID,
			MemberVisibility: normalized,
			UpdatedAt:        now,
		}},
		options.Update().SetUpsert(true),
	)
	return errs.Wrap(err)
}

func (c *ContactMgo) nextSequence(ctx context.Context, seqDocID string) (int64, error) {
	if c.metaColl == nil {
		return time.Now().UnixMilli(), nil
	}
	now := time.Now().UnixMilli()
	var doc orgMetaDoc
	err := c.metaColl.FindOneAndUpdate(
		ctx,
		bson.M{"_id": seqDocID},
		bson.M{
			"$inc": bson.M{"seq": int64(1)},
			"$set": bson.M{"updated_at": now},
		},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&doc)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return doc.Seq, nil
}

func (c *ContactMgo) EnsureOrgCode(ctx context.Context) (string, error) {
	if c.metaColl == nil {
		return "O00001", nil
	}
	var doc orgMetaDoc
	if err := c.metaColl.FindOne(ctx, bson.M{"_id": orgCodeMetaDocID}).Decode(&doc); err == nil && doc.Code != "" {
		return doc.Code, nil
	}
	seq, err := c.nextSequence(ctx, orgCodeSeqDocID)
	if err != nil {
		return "", err
	}
	candidate := fmt.Sprintf("O%05d", seq)
	now := time.Now().UnixMilli()
	_, err = c.metaColl.UpdateOne(
		ctx,
		bson.M{"_id": orgCodeMetaDocID},
		bson.M{
			"$setOnInsert": orgMetaDoc{
				ID:        orgCodeMetaDocID,
				Code:      candidate,
				UpdatedAt: now,
			},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return "", errs.Wrap(err)
	}
	if err := c.metaColl.FindOne(ctx, bson.M{"_id": orgCodeMetaDocID}).Decode(&doc); err == nil && doc.Code != "" {
		return doc.Code, nil
	}
	return candidate, nil
}

func (c *ContactMgo) AllocateDeptCode(ctx context.Context) (string, error) {
	for i := 0; i < 16; i++ {
		seq, err := c.nextSequence(ctx, deptCodeSeqDocID)
		if err != nil {
			return "", err
		}
		code := fmt.Sprintf("D%05d", seq)
		cnt, err := c.deptColl.CountDocuments(ctx, bson.M{"dept_code": code}, options.Count().SetLimit(1))
		if err != nil {
			return "", errs.Wrap(err)
		}
		if cnt == 0 {
			return code, nil
		}
	}
	return "", errs.ErrInternalServer.WrapMsg("failed to allocate unique department code")
}

func (c *ContactMgo) AllocateEmployeeNo(ctx context.Context) (string, error) {
	for i := 0; i < 64; i++ {
		seq, err := c.nextSequence(ctx, empNoSeqDocID)
		if err != nil {
			return "", err
		}
		empNo := fmt.Sprintf("E%05d", seq)
		if c.empRecordColl == nil {
			return empNo, nil
		}
		cnt, err := c.empRecordColl.CountDocuments(ctx, bson.M{"employee_no": empNo}, options.Count().SetLimit(1))
		if err != nil {
			return "", errs.Wrap(err)
		}
		if cnt == 0 {
			return empNo, nil
		}
	}
	return "", errs.ErrInternalServer.WrapMsg("failed to allocate unique employee number")
}

func (c *ContactMgo) CreateEmployeeRecord(ctx context.Context, rec *model.OrgEmployeeRecord) error {
	if rec == nil {
		return nil
	}
	now := time.Now().UnixMilli()
	if strings.TrimSpace(rec.RecordID) == "" {
		rec.RecordID = "emprec_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	}
	if rec.Status == 0 {
		rec.Status = model.OrgEmployeeStatusActive
	}
	if rec.JoinedAt == 0 {
		rec.JoinedAt = now
	}
	rec.UpdatedAt = now
	_, err := c.empRecordColl.InsertOne(ctx, rec)
	return errs.Wrap(err)
}

func (c *ContactMgo) UpdateActiveEmployeeRecord(ctx context.Context, userID, nickname, jobTitle string, deptNames []string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" || c.empRecordColl == nil {
		return nil
	}
	setFields := bson.M{
		"updated_at": time.Now().UnixMilli(),
	}
	if nickname != "" {
		setFields["nickname"] = nickname
	}
	setFields["job_title"] = jobTitle
	if deptNames != nil {
		setFields["dept_names"] = deptNames
	}
	_, err := c.empRecordColl.UpdateMany(
		ctx,
		bson.M{"user_id": userID, "status": model.OrgEmployeeStatusActive},
		bson.M{"$set": setFields},
	)
	return errs.Wrap(err)
}

func (c *ContactMgo) MarkEmployeeRecordRemoved(
	ctx context.Context,
	userID string,
	removedAt int64,
	reason, removedByUserID, removedByAccount string,
	lastDeptNames []string,
) (int64, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || c.empRecordColl == nil {
		return 0, nil
	}
	if removedAt <= 0 {
		removedAt = time.Now().UnixMilli()
	}
	setFields := bson.M{
		"status":             model.OrgEmployeeStatusRemoved,
		"removed_at":         removedAt,
		"removed_reason":     strings.TrimSpace(reason),
		"removed_by_user_id": strings.TrimSpace(removedByUserID),
		"removed_by_account": strings.TrimSpace(removedByAccount),
		"updated_at":         removedAt,
	}
	if len(lastDeptNames) > 0 {
		setFields["dept_names"] = lastDeptNames
	}
	res, err := c.empRecordColl.UpdateMany(
		ctx,
		bson.M{"user_id": userID, "status": model.OrgEmployeeStatusActive},
		bson.M{"$set": setFields},
	)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return res.ModifiedCount, nil
}

func (c *ContactMgo) ListUserEmployeeRecords(ctx context.Context, userID string) ([]*model.OrgEmployeeRecord, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" || c.empRecordColl == nil {
		return []*model.OrgEmployeeRecord{}, nil
	}
	opts := options.Find().SetSort(bson.D{{Key: "joined_at", Value: -1}, {Key: "updated_at", Value: -1}})
	recs, err := mongoutil.Find[*model.OrgEmployeeRecord](ctx, c.empRecordColl, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return recs, nil
}

func (c *ContactMgo) ListAllEmployeeRecords(ctx context.Context) ([]*model.OrgEmployeeRecord, error) {
	if c.empRecordColl == nil {
		return []*model.OrgEmployeeRecord{}, nil
	}
	opts := options.Find().SetSort(bson.D{{Key: "updated_at", Value: -1}, {Key: "joined_at", Value: -1}})
	recs, err := mongoutil.Find[*model.OrgEmployeeRecord](ctx, c.empRecordColl, bson.M{}, opts)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return recs, nil
}

func (c *ContactMgo) AreInSameOrgDirectory(ctx context.Context, userA, userB string) (bool, error) {
	cntA, err := c.memberColl.CountDocuments(ctx, bson.M{"user_id": userA}, options.Count().SetLimit(1))
	if err != nil {
		return false, errs.Wrap(err)
	}
	if cntA == 0 {
		return false, nil
	}
	cntB, err := c.memberColl.CountDocuments(ctx, bson.M{"user_id": userB}, options.Count().SetLimit(1))
	if err != nil {
		return false, errs.Wrap(err)
	}
	return cntB > 0, nil
}

func (c *ContactMgo) ListRelations(ctx context.Context, ownerUserID string, updatedAfterMs int64) ([]*model.ContactRelation, error) {
	filter := bson.M{"owner_user_id": ownerUserID}
	if updatedAfterMs > 0 {
		filter["updated_at"] = bson.M{"$gt": updatedAfterMs}
	}
	opts := options.Find().SetSort(bson.D{{Key: "is_favorite", Value: -1}, {Key: "updated_at", Value: -1}})
	rels, err := mongoutil.Find[*model.ContactRelation](ctx, c.relColl, filter, opts)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return rels, nil
}

func (c *ContactMgo) GetRelation(ctx context.Context, ownerUserID, targetUserID string) (*model.ContactRelation, error) {
	rel, err := mongoutil.FindOne[*model.ContactRelation](ctx, c.relColl, bson.M{
		"owner_user_id":  ownerUserID,
		"target_user_id": targetUserID,
	})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errs.ErrRecordNotFound.WrapMsg("contact relation not found")
		}
		return nil, errs.Wrap(err)
	}
	return rel, nil
}

func (c *ContactMgo) UpsertRelation(ctx context.Context, rel *model.ContactRelation) error {
	now := time.Now().UnixMilli()
	if rel.CreatedAt == 0 {
		rel.CreatedAt = now
	}
	rel.UpdatedAt = now
	if rel.GroupIDs == nil {
		rel.GroupIDs = []string{}
	}
	_, err := c.relColl.UpdateOne(ctx,
		bson.M{"owner_user_id": rel.OwnerUserID, "target_user_id": rel.TargetUserID},
		bson.M{
			"$set": bson.M{
				"remark":      rel.Remark,
				"is_favorite": rel.IsFavorite,
				"is_blocked":  rel.IsBlocked,
				"group_ids":   rel.GroupIDs,
				"source_type": rel.SourceType,
				"updated_at":  rel.UpdatedAt,
			},
			"$setOnInsert": bson.M{
				"owner_user_id":  rel.OwnerUserID,
				"target_user_id": rel.TargetUserID,
				"created_at":     rel.CreatedAt,
			},
		},
		options.Update().SetUpsert(true),
	)
	return errs.Wrap(err)
}

func (c *ContactMgo) RemoveRelation(ctx context.Context, ownerUserID, targetUserID string) error {
	_, err := c.relColl.DeleteOne(ctx, bson.M{"owner_user_id": ownerUserID, "target_user_id": targetUserID})
	return errs.Wrap(err)
}

func (c *ContactMgo) IsBlocked(ctx context.Context, ownerUserID, targetUserID string) (bool, error) {
	cnt, err := c.relColl.CountDocuments(ctx, bson.M{
		"owner_user_id":  ownerUserID,
		"target_user_id": targetUserID,
		"is_blocked":     true,
	}, options.Count().SetLimit(1))
	if err != nil {
		return false, errs.Wrap(err)
	}
	return cnt > 0, nil
}

func (c *ContactMgo) ListGroups(ctx context.Context, ownerUserID string) ([]*model.ContactGroup, error) {
	opts := options.Find().SetSort(bson.D{{Key: "sort_order", Value: 1}, {Key: "updated_at", Value: 1}})
	groups, err := mongoutil.Find[*model.ContactGroup](ctx, c.groupColl, bson.M{"owner_user_id": ownerUserID}, opts)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return groups, nil
}

func (c *ContactMgo) UpsertGroup(ctx context.Context, group *model.ContactGroup) error {
	group.UpdatedAt = time.Now().UnixMilli()
	_, err := c.groupColl.UpdateOne(ctx,
		bson.M{"group_id": group.GroupID, "owner_user_id": group.OwnerUserID},
		bson.M{"$set": group},
		options.Update().SetUpsert(true),
	)
	return errs.Wrap(err)
}

func (c *ContactMgo) DeleteGroup(ctx context.Context, ownerUserID, groupID string) error {
	if _, err := c.groupColl.DeleteOne(ctx, bson.M{"group_id": groupID, "owner_user_id": ownerUserID}); err != nil {
		return errs.Wrap(err)
	}
	// Remove dangling group_id reference from user's contact relations.
	_, err := c.relColl.UpdateMany(ctx,
		bson.M{"owner_user_id": ownerUserID, "group_ids": groupID},
		bson.M{"$pull": bson.M{"group_ids": groupID}, "$set": bson.M{"updated_at": time.Now().UnixMilli()}},
	)
	return errs.Wrap(err)
}

func (c *ContactMgo) CreateRequest(ctx context.Context, req *model.ContactRequest) error {
	if req.CreatedAt == 0 {
		req.CreatedAt = time.Now().UnixMilli()
	}
	_, err := c.requestColl.InsertOne(ctx, req)
	return errs.Wrap(err)
}

func (c *ContactMgo) GetRequest(ctx context.Context, requestID string) (*model.ContactRequest, error) {
	req, err := mongoutil.FindOne[*model.ContactRequest](ctx, c.requestColl, bson.M{"request_id": requestID})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errs.ErrRecordNotFound.WrapMsg("contact request not found")
		}
		return nil, errs.Wrap(err)
	}
	return req, nil
}

func (c *ContactMgo) FindPendingRequest(ctx context.Context, fromUserID, toUserID string) (*model.ContactRequest, error) {
	req, err := mongoutil.FindOne[*model.ContactRequest](ctx, c.requestColl, bson.M{
		"from_user_id": fromUserID,
		"to_user_id":   toUserID,
		"status":       model.ContactRequestPending,
	})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, errs.Wrap(err)
	}
	return req, nil
}

func (c *ContactMgo) UpdateRequestStatus(ctx context.Context, requestID string, expectedStatus, newStatus int32, handledAt int64) (bool, error) {
	res, err := c.requestColl.UpdateOne(ctx,
		bson.M{"request_id": requestID, "status": expectedStatus},
		bson.M{"$set": bson.M{"status": newStatus, "handled_at": handledAt}},
	)
	if err != nil {
		return false, errs.Wrap(err)
	}
	return res.ModifiedCount > 0, nil
}

func (c *ContactMgo) ListRequests(ctx context.Context, userID string, limit int64) ([]*model.ContactRequest, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	filter := bson.M{
		"$or": []bson.M{
			{"to_user_id": userID},
			{"from_user_id": userID},
		},
	}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit)
	reqs, err := mongoutil.Find[*model.ContactRequest](ctx, c.requestColl, filter, opts)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return reqs, nil
}
