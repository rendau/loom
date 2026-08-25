package scheduler

import (
	"context"
	"time"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	secretModel "github.com/rendau/loom/server/internal/domain/secret/model"
	settingModel "github.com/rendau/loom/server/internal/domain/setting/model"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
	variableModel "github.com/rendau/loom/server/internal/domain/variable/model"
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
	SetAttemptPeakMemory(ctx context.Context, ref runModel.AttemptRef, peakBytes int64) error
	FinishRun(ctx context.Context, runId, status string) (bool, error)
	Trigger(ctx context.Context, dag *dagModel.Main, spec runModel.TriggerSpec) (string, error)
	CountActiveTaskInstances(ctx context.Context) (map[string]int64, error)
	ListPoolUsage(ctx context.Context) ([]runModel.PoolUsage, error)
	// SaveRunEnv — снапшот фактического env-резолва при launch попытки.
	SaveRunEnv(ctx context.Context, runId string, entries []runModel.RunEnv) error
}

// DagServiceI — cron-расписания: выборка дагов с наступившим next_run_at и
// его сдвиг compare-and-swap'ом (защита от двойного триггера при нескольких
// инстансах control plane). GetTaskResources — оверрайд ресурсов таска из
// админки, накладывается на манифест при launch (nil — оверрайда нет).
type DagServiceI interface {
	ListDueSchedules(ctx context.Context) ([]*dagModel.Main, error)
	AdvanceNextRun(ctx context.Context, name string, from, to time.Time) (bool, error)
	GetTaskResources(ctx context.Context, dagName, task string) (*dagModel.TaskResources, error)
}

// SettingsI — эффективные настройки скоупа дага для launch (TTL k8s
// Job'ов).
type SettingsI interface {
	Resolve(ctx context.Context, dagName string) (settingModel.Effective, error)
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

// SecretResolverI — расшифровка значений секретов для env попытки (скоуп
// дага: локальный перекрывает глобальный, скоуп-источник уходит в снапшот
// run_env); отсутствующий секрет — ошибка (Launch не должен стартовать
// попытку с пустой переменной).
type SecretResolverI interface {
	ResolveValues(ctx context.Context, dagName string, names []string) (map[string]secretModel.Resolved, error)
}

// VariableResolverI — значения переменных для env попытки; семантика скоупа
// и отсутствия — как у секретов.
type VariableResolverI interface {
	ResolveValues(ctx context.Context, dagName string, names []string) (map[string]variableModel.Resolved, error)
}

// TaskLogI — финализация лог-стрима попытки (на artifact-сервере) с
// дописыванием исхода попытки.
type TaskLogI interface {
	Finish(ctx context.Context, key tasklogModel.AttemptKey, final []tasklogModel.Entry) error
}
