package service

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/dag/model"
)

type RepoDbI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, name string) (*model.Main, bool, error)
	UpdateOrCreate(ctx context.Context, name string, obj *model.Edit) error
	Update(ctx context.Context, name string, obj *model.Edit) error
	Delete(ctx context.Context, name string) error
}
