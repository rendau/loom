package service

import (
	"context"
	"time"

	"github.com/rendau/loom/server/internal/domain/dag/model"
)

type RepoDbI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, ref model.Ref) (*model.Main, bool, error)
	Create(ctx context.Context, ref model.Ref, obj *model.Edit) error
	Update(ctx context.Context, ref model.Ref, obj *model.Edit) error
	Delete(ctx context.Context, ref model.Ref) error

	SetTaskResources(ctx context.Context, ref model.Ref, task string, res model.TaskResources) error
	DeleteTaskResources(ctx context.Context, ref model.Ref, task string) (bool, error)
	ListTaskResources(ctx context.Context, ref model.Ref) ([]*model.TaskResourcesEntry, error)
	GetTaskResources(ctx context.Context, ref model.Ref, task string) (*model.TaskResources, error)

	SetNextRun(ctx context.Context, ref model.Ref, t *time.Time) error
	ListLastRuns(ctx context.Context, refs []model.Ref, perDag int) (map[model.Ref][]model.LastRun, error)
	ListDueRefs(ctx context.Context) ([]model.Ref, error)
	AdvanceNextRun(ctx context.Context, ref model.Ref, from time.Time, to time.Time) (bool, error)
}
