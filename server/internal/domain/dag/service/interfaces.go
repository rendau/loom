package service

import (
	"context"
	"time"

	"github.com/rendau/loom/server/internal/domain/dag/model"
)

type RepoDbI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, name string) (*model.Main, bool, error)
	UpdateOrCreate(ctx context.Context, name string, obj *model.Edit) error
	Update(ctx context.Context, name string, obj *model.Edit) error
	Delete(ctx context.Context, name string) error

	SetNextRun(ctx context.Context, name string, t *time.Time) error
	ListDueNames(ctx context.Context) ([]string, error)
	AdvanceNextRun(ctx context.Context, name string, from time.Time, to time.Time) (bool, error)
}
