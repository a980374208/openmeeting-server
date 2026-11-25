package database

import (
	"context"
	"github.com/openimsdk/openmeeting-server/pkg/common/storage/model"
)

type User interface {
	Create(ctx context.Context, users []*model.User) (err error)
	Take(ctx context.Context, userID string) (user *model.User, err error)
	TakeByAccount(ctx context.Context, account string) (user *model.User, err error)
	TakeByExactIdentifier(ctx context.Context, identifier string) (*model.User, error)
	FindByUserIDs(ctx context.Context, userIDs []string) ([]*model.User, error)
	UpdateProfile(ctx context.Context, userID string, update map[string]any) error
	UpsertByAccount(ctx context.Context, user *model.User) error
	SearchByPrefix(ctx context.Context, keyword string, deptID string, restrictUserIDs []string, offset, limit int64) ([]*model.User, int64, error)
	ListAll(ctx context.Context, keyword string, offset, limit int64) ([]*model.User, int64, error)
}
