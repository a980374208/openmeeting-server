// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mgo

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/openimsdk/openmeeting-server/pkg/common/storage/database"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewUserMongo(db *mongo.Database) (database.User, error) {
	coll := db.Collection("user")
	_, err := coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "account", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{
				"account": bson.M{"$type": "string", "$gt": ""},
			}),
		},
		{
			Keys: bson.D{{Key: "search_tokens", Value: 1}, {Key: "status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "primary_dept_ids", Value: 1}, {Key: "status", Value: 1}},
		},
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &UserMgo{coll: coll}, nil
}

type UserMgo struct {
	coll *mongo.Collection
}

func (u *UserMgo) Create(ctx context.Context, users []*model.User) error {
	now := time.Now().UnixMilli()
	for _, user := range users {
		if user.UpdatedAt == 0 {
			user.UpdatedAt = now
		}
		if len(user.SearchTokens) == 0 {
			user.SearchTokens = model.BuildSearchTokens(user)
		}
	}
	return mongoutil.InsertMany(ctx, u.coll, users)
}

func (u *UserMgo) Take(ctx context.Context, userID string) (user *model.User, err error) {
	return mongoutil.FindOne[*model.User](ctx, u.coll, bson.M{"user_id": userID})
}

func (u *UserMgo) TakeByAccount(ctx context.Context, account string) (user *model.User, err error) {
	return mongoutil.FindOne[*model.User](ctx, u.coll, bson.M{"account": account})
}

func (u *UserMgo) TakeByExactIdentifier(ctx context.Context, identifier string) (*model.User, error) {
	id := strings.TrimSpace(identifier)
	if id == "" {
		return nil, errs.ErrRecordNotFound.WrapMsg("identifier is empty")
	}
	filter := bson.M{
		"status": bson.M{"$ne": model.UserStatusDisabled},
		"$or": []bson.M{
			{"account": id},
			{"employee_no": id},
			{"email": id},
			{"user_id": id},
		},
	}
	user, err := mongoutil.FindOne[*model.User](ctx, u.coll, filter)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errs.ErrRecordNotFound.WrapMsg("user not found")
		}
		return nil, errs.Wrap(err)
	}
	return user, nil
}

func (u *UserMgo) FindByUserIDs(ctx context.Context, userIDs []string) ([]*model.User, error) {
	if len(userIDs) == 0 {
		return []*model.User{}, nil
	}
	users, err := mongoutil.Find[*model.User](ctx, u.coll, bson.M{"user_id": bson.M{"$in": userIDs}})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return users, nil
}

func (u *UserMgo) UpdateProfile(ctx context.Context, userID string, update map[string]any) error {
	if len(update) == 0 {
		return nil
	}
	update["updated_at"] = time.Now().UnixMilli()
	return mongoutil.UpdateOne(ctx, u.coll, bson.M{"user_id": userID}, bson.M{"$set": update}, false)
}

func (u *UserMgo) UpsertByAccount(ctx context.Context, user *model.User) error {
	user.UpdatedAt = time.Now().UnixMilli()
	if len(user.SearchTokens) == 0 {
		user.SearchTokens = model.BuildSearchTokens(user)
	}
	setDoc := bson.M{
		"nickname":         user.Nickname,
		"employee_no":      user.EmployeeNo,
		"email":            user.Email,
		"phone":            user.Phone,
		"job_title":        user.JobTitle,
		"signature":        user.Signature,
		"avatar_url":       user.AvatarURL,
		"primary_dept_ids": user.PrimaryDeptIDs,
		"search_tokens":    user.SearchTokens,
		"status":           user.Status,
		"updated_at":       user.UpdatedAt,
	}
	if user.Password != "" {
		setDoc["password"] = user.Password
		setDoc["salt_value"] = user.SaltValue
	}
	_, err := u.coll.UpdateOne(ctx,
		bson.M{"account": user.Account},
		bson.M{
			"$set": setDoc,
			"$setOnInsert": bson.M{
				"user_id": user.UserID,
				"account": user.Account,
			},
		},
		options.Update().SetUpsert(true),
	)
	return errs.Wrap(err)
}

// SearchByPrefix performs ReDoS-safe anchored prefix search on search_tokens
// (falling back to escaped nickname/account prefix for legacy rows without tokens).
func (u *UserMgo) SearchByPrefix(ctx context.Context, keyword string, deptID string, restrictUserIDs []string, offset, limit int64) ([]*model.User, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	if offset < 0 {
		offset = 0
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if len([]rune(kw)) > 64 {
		kw = string([]rune(kw)[:64])
	}
	filter := bson.M{
		"status": bson.M{"$ne": model.UserStatusDisabled},
	}
	if deptID != "" {
		filter["primary_dept_ids"] = deptID
	}
	if restrictUserIDs != nil {
		if len(restrictUserIDs) == 0 {
			return []*model.User{}, 0, nil
		}
		filter["user_id"] = bson.M{"$in": restrictUserIDs}
	}
	if kw != "" {
		anchored := primitive.Regex{Pattern: "^" + regexp.QuoteMeta(kw), Options: ""}
		anchoredCI := primitive.Regex{Pattern: "^" + regexp.QuoteMeta(kw), Options: "i"}
		filter["$or"] = []bson.M{
			{"search_tokens": anchored},
			{"nickname": anchoredCI},
			{"account": anchoredCI},
			{"employee_no": anchoredCI},
		}
	}
	total, err := u.coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "nickname", Value: 1}, {Key: "user_id", Value: 1}}).
		SetSkip(offset).
		SetLimit(limit)
	users, err := mongoutil.Find[*model.User](ctx, u.coll, filter, opts)
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return users, total, nil
}

func (u *UserMgo) ListAll(ctx context.Context, keyword string, offset, limit int64) ([]*model.User, int64, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	filter := bson.M{
		"status": bson.M{"$ne": model.UserStatusDisabled},
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw != "" {
		if len([]rune(kw)) > 64 {
			kw = string([]rune(kw)[:64])
		}
		quoted := regexp.QuoteMeta(kw)
		filter["$or"] = []bson.M{
			{"search_tokens": primitive.Regex{Pattern: "^" + quoted, Options: ""}},
			{"nickname": primitive.Regex{Pattern: quoted, Options: "i"}},
			{"account": primitive.Regex{Pattern: quoted, Options: "i"}},
			{"employee_no": primitive.Regex{Pattern: quoted, Options: "i"}},
			{"email": primitive.Regex{Pattern: quoted, Options: "i"}},
		}
	}
	total, err := u.coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "account", Value: 1}, {Key: "user_id", Value: 1}}).
		SetSkip(offset).
		SetLimit(limit)
	users, err := mongoutil.Find[*model.User](ctx, u.coll, filter, opts)
	if err != nil {
		return nil, 0, errs.Wrap(err)
	}
	return users, total, nil
}
