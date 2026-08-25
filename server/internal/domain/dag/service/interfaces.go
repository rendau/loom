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

	SetTaskResources(ctx context.Context, dagName, task string, res model.TaskResources) error
	DeleteTaskResources(ctx context.Context, dagName, task string) (bool, error)
	ListTaskResources(ctx context.Context, dagName string) ([]*model.TaskResourcesEntry, error)
	GetTaskResources(ctx context.Context, dagName, task string) (*model.TaskResources, error)

	SetNextRun(ctx context.Context, name string, t *time.Time) error
	ListLastRuns(ctx context.Context, dagNames []string, perDag int) (map[string][]model.LastRun, error)
	ListDueNames(ctx context.Context) ([]string, error)
	AdvanceNextRun(ctx context.Context, name string, from time.Time, to time.Time) (bool, error)
}
