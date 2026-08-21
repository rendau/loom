package service

import (
	"context"
	"time"

	"github.com/rendau/loom/server/internal/domain/run/model"
)

type RepoDbI interface {
	ListRuns(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	GetRun(ctx context.Context, id string) (*model.Main, bool, error)
	CreateRun(ctx context.Context, obj *model.Main) error
	UpdateRun(ctx context.Context, id string, obj *model.Edit) error
	FinishRun(ctx context.Context, runId, status string) error

	CreateTaskInstances(ctx context.Context, runId string, tasks []string) error
	ListTaskInstances(ctx context.Context, runId string) ([]*model.TaskInstance, error)
	PromoteTaskInstances(ctx context.Context, runId string, tasks []string, fromStatus, toStatus string) error
	ClaimQueuedTasks(ctx context.Context, limit int64) ([]model.ClaimedTask, error)
	PromoteRetries(ctx context.Context) (int64, error)

	CreateAttempt(ctx context.Context, ref model.AttemptRef) error
	GetAttempt(ctx context.Context, ref model.AttemptRef) (*model.Attempt, bool, error)
	ListAttempts(ctx context.Context, runId string) ([]*model.Attempt, error)
	ListStaleAttempts(ctx context.Context, olderThan time.Time) ([]model.StaleAttempt, error)
	MarkAttemptRunning(ctx context.Context, ref model.AttemptRef) (bool, error)
	FinalizeAttempt(ctx context.Context, ref model.AttemptRef, exit model.ExitInfo, retryAt *time.Time) (bool, error)
}

type TxManagerI interface {
	TxFn(ctx context.Context, f func(context.Context) error) error
}
