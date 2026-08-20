package scheduler

import (
	"context"

	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
)

type RunServiceI interface {
	Get(ctx context.Context, id string, errNE bool) (*runModel.Main, bool, error)
	ListActive(ctx context.Context) ([]*runModel.Main, error)
	ListTaskInstances(ctx context.Context, runId string) ([]*runModel.TaskInstance, error)
	PromoteTasks(ctx context.Context, runId string, tasks []string, fromStatus, toStatus string) error
	ClaimQueued(ctx context.Context, limit int64) ([]runModel.ClaimedTask, error)
	MarkAttemptRunning(ctx context.Context, ref runModel.AttemptRef) (bool, error)
	FinalizeAttempt(ctx context.Context, ref runModel.AttemptRef, exit runModel.ExitInfo) (bool, error)
	FinishRun(ctx context.Context, runId, status string) error
}

// ExecutorI — порт executor'а (решение №8): запуск/остановка на уровне
// attempt'а, события жизненного цикла — каналом.
type ExecutorI interface {
	Launch(ctx context.Context, spec runModel.LaunchSpec) error
	Kill(ctx context.Context, ref runModel.AttemptRef) error
	Events() <-chan runModel.ExecEvent
}

// ArtifactI — клиент artifact-сервера: страховочный FinishAttempt при
// завершении/смерти попытки (решение №13).
type ArtifactI interface {
	FinishAttempt(ctx context.Context, ref runModel.AttemptRef) error
}

// TaskLogI — финализация лог-стрима попытки с дописыванием причины смерти.
type TaskLogI interface {
	Finish(key tasklogModel.AttemptKey, final []tasklogModel.Entry) error
}
