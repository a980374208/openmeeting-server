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

package controller

import (
	"context"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/tx"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/utils/datautil"

	"github.com/openimsdk/openmeeting-server/pkg/common/storage/cache"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/database"
)

type User interface {
	// FindWithError Get the information of the specified user. If the userID is not found, it will also return an error
	FindWithError(ctx context.Context, userIDs []string) (users []*model.User, err error) //1
	// Create Insert multiple external guarantees that the userID is not repeated and does not exist in the storage
	Create(ctx context.Context, users []*model.User) (err error) //1
	// GetByAccount Get user by account
	GetByAccount(ctx context.Context, account string) (*model.User, error)

	// StoreToken cache in storage
	StoreToken(ctx context.Context, userID, userToken string) error

	// GetToken get cache from storage
	GetToken(ctx context.Context, userID string) (string, error)

	// ClearUserToken clear cache from storage
	ClearUserToken(ctx context.Context, userID string) error

	// GenerateUserID generate a unique user id
	GenerateUserID(ctx context.Context) (string, error)

	TakeByExactIdentifier(ctx context.Context, identifier string) (*model.User, error)
	FindByUserIDs(ctx context.Context, userIDs []string) ([]*model.User, error)
	UpdateProfile(ctx context.Context, userID string, account string, update map[string]any) error
	UpsertByAccount(ctx context.Context, user *model.User) error
	SearchByPrefix(ctx context.Context, keyword string, deptID string, restrictUserIDs []string, offset, limit int64) ([]*model.User, int64, error)
	ListAll(ctx context.Context, keyword string, offset, limit int64) ([]*model.User, int64, error)
}

type UserStorageManager struct {
	tx    tx.Tx
	db    database.User
	cache cache.User
}

func NewUser(userDB database.User, cache cache.User, tx tx.Tx) User {
	return &UserStorageManager{db: userDB, cache: cache, tx: tx}
}

// FindWithError Get the information of the specified user and return an error if the userID is not found.
func (u *UserStorageManager) FindWithError(ctx context.Context, userIDs []string) (users []*model.User, err error) {
	users, err = u.cache.GetUsersInfo(ctx, userIDs)
	if err != nil {
		return
	}
	if len(users) != len(userIDs) {
		err = errs.ErrRecordNotFound.WrapMsg("userID not found")
	}
	return
}

// Create Insert multiple external guarantees that the userID is not repeated and does not exist in the storage.
func (u *UserStorageManager) Create(ctx context.Context, users []*model.User) (err error) {
	return u.tx.Transaction(ctx, func(ctx context.Context) error {
		if err = u.db.Create(ctx, users); err != nil {
			return err
		}
		return u.cache.DelUsersInfo(datautil.Slice(users, func(e *model.User) string {
			return e.UserID
		})...).ExecDel(ctx)
	})
}

func (u *UserStorageManager) GetByAccount(ctx context.Context, account string) (user *model.User, err error) {
	user, err = u.cache.GetUserByAccount(ctx, account)
	if err != nil {
		return
	}
	if user == nil {
		err = errs.ErrRecordNotFound.WrapMsg("account not found: ", account)
	}
	return
}

func (u *UserStorageManager) StoreToken(ctx context.Context, userID, userToken string) error {
	return u.cache.CacheUserToken(ctx, userID, userToken)
}

func (u *UserStorageManager) GetToken(ctx context.Context, userID string) (string, error) {
	token, err := u.cache.GetUserToken(ctx, userID)
	if err != nil {
		return "", errs.Wrap(err)
	}
	return token, nil
}

func (u *UserStorageManager) ClearUserToken(ctx context.Context, userID string) error {
	return u.cache.ClearUserToken(ctx, userID)
}

func (u *UserStorageManager) GenerateUserID(ctx context.Context) (string, error) {
	return u.cache.GenerateUserID(ctx)
}

func (u *UserStorageManager) TakeByExactIdentifier(ctx context.Context, identifier string) (*model.User, error) {
	return u.db.TakeByExactIdentifier(ctx, identifier)
}

func (u *UserStorageManager) FindByUserIDs(ctx context.Context, userIDs []string) ([]*model.User, error) {
	return u.db.FindByUserIDs(ctx, userIDs)
}

func (u *UserStorageManager) UpdateProfile(ctx context.Context, userID string, account string, update map[string]any) error {
	return u.tx.Transaction(ctx, func(ctx context.Context) error {
		if err := u.db.UpdateProfile(ctx, userID, update); err != nil {
			return err
		}
		keys := []string{userID}
		if account != "" {
			keys = append(keys, account)
		}
		return u.cache.DelUsersInfo(keys...).ExecDel(ctx)
	})
}

func (u *UserStorageManager) UpsertByAccount(ctx context.Context, user *model.User) error {
	return u.tx.Transaction(ctx, func(ctx context.Context) error {
		if err := u.db.UpsertByAccount(ctx, user); err != nil {
			return err
		}
		keys := []string{user.Account}
		if user.UserID != "" {
			keys = append(keys, user.UserID)
		}
		return u.cache.DelUsersInfo(keys...).ExecDel(ctx)
	})
}

func (u *UserStorageManager) SearchByPrefix(ctx context.Context, keyword string, deptID string, restrictUserIDs []string, offset, limit int64) ([]*model.User, int64, error) {
	return u.db.SearchByPrefix(ctx, keyword, deptID, restrictUserIDs, offset, limit)
}

func (u *UserStorageManager) ListAll(ctx context.Context, keyword string, offset, limit int64) ([]*model.User, int64, error) {
	return u.db.ListAll(ctx, keyword, offset, limit)
}
