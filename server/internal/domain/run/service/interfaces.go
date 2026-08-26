package service

import (
	"context"
	"time"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/run/model"
)

type RepoDbI interface {
	ListRuns(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	CountRunsByStatus(ctx context.Context, ref *dagModel.Ref) (map[string]int64, error)
	GetRun(ctx context.Context, id string) (*model.Main, bool, error)
	CreateRun(ctx context.Context, obj *model.Main) error
	UpdateRun(ctx context.Context, id string, obj *model.Edit) error
	FinishRun(ctx context.Context, runId, status string) (bool, error)
	ListRetentionDags(ctx context.Context) ([]dagModel.Ref, error)
	ListExpiredRuns(ctx context.Context, ref dagModel.Ref, before *time.Time, keepLast, limit int64) ([]string, error)
	DeleteRun(ctx context.Context, runId string) error
	CountActiveTaskInstances(ctx context.Context) (map[string]int64, error)
	ListPoolUsage(ctx context.Context) ([]model.PoolUsage, error)

	CreateTaskInstances(ctx context.Context, runId string, tasks []model.TaskSeed) error
	ListTaskInstances(ctx context.Context, runId string) ([]*model.TaskInstance, error)
	PromoteTaskInstances(ctx context.Context, runId string, tasks []string, fromStatus, toStatus string) error
	ClaimQueuedTasks(ctx context.Context, limit int64) ([]model.ClaimedTask, error)
	PromoteRetries(ctx context.Context) (int64, error)
	RetryTaskSubgraph(ctx context.Context, runId, task string, downstream []string) (bool, error)
	CancelRun(ctx context.Context, runId string) (bool, []model.AttemptRef, error)

	UpsertTaskValue(ctx context.Context, v *model.TaskValue) error
	GetTaskValue(ctx context.Context, runId, task, key string) (*model.TaskValue, bool, error)
	ListTaskValues(ctx context.Context, runId string) ([]*model.TaskValue, error)

	UpsertRunEnv(ctx context.Context, runId string, entries []model.RunEnv) error
	ListRunEnv(ctx context.Context, runId string) ([]model.RunEnv, error)

	CreateAttempt(ctx context.Context, ref model.AttemptRef) error
	GetAttempt(ctx context.Context, ref model.AttemptRef) (*model.Attempt, bool, error)
	ListAttempts(ctx context.Context, runId string) ([]*model.Attempt, error)
	ListStaleAttempts(ctx context.Context, olderThan time.Time) ([]model.StaleAttempt, error)
	MarkAttemptRunning(ctx context.Context, ref model.AttemptRef) (bool, error)
	FinalizeAttempt(ctx context.Context, ref model.AttemptRef, exit model.ExitInfo, retryAt *time.Time) (bool, *time.Time, error)
	SetAttemptPeakMemory(ctx context.Context, ref model.AttemptRef, peakBytes int64) error
}

type TxManagerI interface {
	TxFn(ctx context.Context, f func(context.Context) error) error
}
