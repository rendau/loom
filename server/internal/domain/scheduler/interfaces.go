package scheduler

import (
	"context"
	"time"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
)

type RunServiceI interface {
	Get(ctx context.Context, id string, errNE bool) (*runModel.Main, bool, error)
	ListActive(ctx context.Context) ([]*runModel.Main, error)
	ListTaskInstances(ctx context.Context, runId string) ([]*runModel.TaskInstance, error)
	PromoteTasks(ctx context.Context, runId string, tasks []string, fromStatus, toStatus string) error
	ClaimQueued(ctx context.Context, limit int64) ([]runModel.ClaimedTask, error)
	PromoteRetries(ctx context.Context) (int64, error)
	ListStaleAttempts(ctx context.Context, olderThan time.Time) ([]runModel.StaleAttempt, error)
	MarkAttemptRunning(ctx context.Context, ref runModel.AttemptRef) (bool, error)
	FinalizeAttempt(ctx context.Context, ref runModel.AttemptRef, exit runModel.ExitInfo, retryAt *time.Time) (bool, *time.Time, error)
	FinishRun(ctx context.Context, runId, status string) (bool, error)
	Trigger(ctx context.Context, dag *dagModel.Main, spec runModel.TriggerSpec) (string, error)
	CountActiveTaskInstances(ctx context.Context) (map[string]int64, error)
	ListPoolUsage(ctx context.Context) ([]runModel.PoolUsage, error)
}

// DagServiceI — cron-расписания: выборка дагов с наступившим next_run_at и
// его сдвиг compare-and-swap'ом (защита от двойного триггера при нескольких
// инстансах control plane).
type DagServiceI interface {
	ListDueSchedules(ctx context.Context) ([]*dagModel.Main, error)
	AdvanceNextRun(ctx context.Context, name string, from, to time.Time) (bool, error)
}

// ExecutorI — порт executor'а: запуск/остановка на уровне
// attempt'а, события жизненного цикла — каналом. ListAlive — попытки, чьи
// Job'ы ещё существуют (источник правды зомби-детекта).
type ExecutorI interface {
	Launch(ctx context.Context, spec runModel.LaunchSpec) error
	Kill(ctx context.Context, ref runModel.AttemptRef) error
	ListAlive(ctx context.Context) ([]runModel.AttemptRef, error)
	Events() <-chan runModel.ExecEvent
}

// ArtifactI — клиент artifact-сервера: страховочный FinishAttempt при
// завершении/смерти попытки.
type ArtifactI interface {
	FinishAttempt(ctx context.Context, ref runModel.AttemptRef) error
}

// SecretResolverI — расшифровка значений секретов для env попытки;
// отсутствующий секрет — ошибка (Launch не должен стартовать попытку с
// пустой переменной).
type SecretResolverI interface {
	ResolveValues(ctx context.Context, names []string) (map[string][]byte, error)
}

// TaskLogI — финализация лог-стрима попытки (на artifact-сервере) с
// дописыванием исхода попытки.
type TaskLogI interface {
	Finish(ctx context.Context, key tasklogModel.AttemptKey, final []tasklogModel.Entry) error
}
