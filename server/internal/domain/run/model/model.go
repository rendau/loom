package model

import (
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// Статусы рана.
const (
	RunStatusRunning = "running"
	RunStatusSuccess = "success"
	RunStatusFailed  = "failed"
	// RunStatusCanceled — ран остановлен вручную (CancelRun); терминальный,
	// но доиграть его ретраем таска можно.
	RunStatusCanceled = "canceled"
)

// Статусы task instance.
//
//	pending → queued → starting → running → success | failed
//	pending → upstream_failed (зависимость упала)
//	starting/running → up_for_retry → queued (остались ретраи, ждём backoff)
//	любой нетерминальный → canceled (ран остановлен вручную)
const (
	TaskStatusPending        = "pending"
	TaskStatusQueued         = "queued"
	TaskStatusStarting       = "starting"
	TaskStatusRunning        = "running"
	TaskStatusUpForRetry     = "up_for_retry"
	TaskStatusSuccess        = "success"
	TaskStatusFailed         = "failed"
	TaskStatusUpstreamFailed = "upstream_failed"
	// TaskStatusCanceled — таск остановлен вместе с раном: либо его живая
	// попытка убита, либо он не успел исполниться.
	TaskStatusCanceled = "canceled"
)

// Статусы попытки.
const (
	AttemptStatusStarting = "starting"
	AttemptStatusRunning  = "running"
	AttemptStatusSuccess  = "success"
	AttemptStatusFailed   = "failed"
)

// Триггеры рана.
const (
	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
	TriggerBackfill = "backfill"
)

// MaxParamsSize — лимит params рана: это конфиг, а не данные;
// данные текут артефактами.
const MaxParamsSize = 64 * 1024

// MaxValueSize — лимит значения таска: значения — для мелочи
// вроде счётчиков и id, данные текут артефактами.
const MaxValueSize = 64 * 1024

// TaskStatusTerminal — терминален ли статус task instance.
func TaskStatusTerminal(status string) bool {
	switch status {
	case TaskStatusSuccess, TaskStatusFailed, TaskStatusUpstreamFailed, TaskStatusCanceled:
		return true
	}
	return false
}

// Main — ран дага. Manifest — снапшот манифеста на момент триггера
// (ран не зависит от последующих перерегистраций дага). Params — параметры
// рана (raw JSON-объект, nil — без параметров), LogicalDate — «дата
// данных».
type Main struct {
	Id  string
	Dag dagModel.Ref
	// Template — имя дага в образе: его получает бинарник таска в LOOM_DAG.
	Template    string
	Image       string // образ для запуска подов: repo@digest
	ImageDigest string
	Trigger     string
	Status      string
	Manifest    []byte
	Params      []byte
	LogicalDate time.Time
	CreatedAt   time.Time
	FinishedAt  time.Time // zero — ран ещё идёт
}

// TriggerSpec — параметры создания рана.
type TriggerSpec struct {
	Trigger     string
	Params      []byte    // raw JSON-объект; nil — без параметров
	LogicalDate time.Time // zero — момент триггера
}

// Edit — мутация рана.
type Edit struct {
	Status     *string
	FinishedAt *time.Time
}

// ListReq — параметры выборки ранов.
type ListReq struct {
	commonModel.ListParams

	// Dag — раны конкретного дага; Project — все раны проекта.
	Dag     *dagModel.Ref
	Project *string
	Status  *string
}

// TaskInstance — таск внутри рана; attempt — номер текущей (последней)
// попытки, 0 — ещё не стартовала. Pool/Priority — денормализация из
// манифеста для claim-запроса очереди.
type TaskInstance struct {
	RunId      string
	Task       string
	Status     string
	Attempt    int32
	Pool       string
	Priority   int32
	QueuedAt   time.Time
	StartedAt  time.Time
	RetryAt    time.Time // для up_for_retry: когда вернуть в очередь
	FinishedAt time.Time
}

// TaskSeed — параметры создания task instance рана.
type TaskSeed struct {
	Task     string
	Pool     string
	Priority int32
}

// Attempt — одна попытка таска: 1 pod = 1 attempt.
type Attempt struct {
	RunId      string
	Task       string
	Attempt    int32
	Status     string
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	ExitCode   *int32 // nil — нет данных (например pod пропал)
	ExitReason string // Error, OOMKilled, launch_failed, ...
	// Пик памяти по семплам executor'а; nil — не измерено (короткая попытка
	// или семплер недоступен).
	PeakMemoryBytes *int64
}

// AttemptRef — идентификация попытки.
type AttemptRef struct {
	RunId   string
	Task    string
	Attempt int32
}

// TaskValue — мелкое значение таска (аналог XCom): скоуп
// (run, task, key), ретрай перезаписывает.
type TaskValue struct {
	RunId      string
	Task       string
	Key        string
	Value      []byte // raw JSON
	ModifiedAt time.Time
}

// Виды записей env-снапшота рана.
const (
	RunEnvKindVariable = "variable"
	RunEnvKindSecret   = "secret"
)

// RunEnv — запись env-снапшота рана: фактический резолв привязки манифеста
// (env-имя → переменная/секрет) на момент launch попытки. Значения
// секретов не сохраняются — только имя и скоуп-источник.
type RunEnv struct {
	Env        string
	Kind       string // RunEnvKindVariable | RunEnvKindSecret
	Name       string
	Scope      commonModel.Scope // источник значения: глобальный, проект или даг
	Value      string            // только у variable
	ResolvedAt time.Time
}

// ClaimedTask — таск, забранный из очереди на запуск.
type ClaimedTask struct {
	RunId   string
	Task    string
	Attempt int32 // номер новой попытки
}

// StaleAttempt — незавершённая попытка старше grace-периода: кандидат на
// зомби-детект (сверку с живыми Job'ами executor'а).
type StaleAttempt struct {
	Ref    AttemptRef
	Status string // starting | running
}

// PoolUsage — занятость пула слотов: Busy — попытки в starting/running.
type PoolUsage struct {
	Pool  string
	Slots int64
	Busy  int64
}

// ExitInfo — результат завершения попытки.
type ExitInfo struct {
	Success  bool
	ExitCode *int32
	Reason   string
	Message  string
	// Canceled — попытку убила остановка рана: сама попытка неуспешна
	// (failed), но её task instance получает canceled, а не failed, и
	// ретраев ему не назначают.
	Canceled bool
}

// LaunchSpec — параметры запуска попытки executor'ом: образ рана (пиннутый
// digest), env-контракт LOOM_* и ресурсы контейнера из манифеста.
type LaunchSpec struct {
	Ref AttemptRef
	// Dag — только для читаемых имён объектов executor'а (имя Job'а/пода);
	// идентификация попытки — по Ref.
	Dag dagModel.Ref
	// Template — имя дага в образе для env LOOM_DAG: образ может нести
	// несколько дагов, и бинарник должен знать, какой из них запускать.
	Template  string
	Image     string
	Env       map[string]string
	Resources *dagModel.TaskResources
	// JobTTL — ttlSecondsAfterFinished Job'а попытки (настройка k8s_job_ttl,
	// скоуп дага перекрывает глобальный); 0 — Job не удаляется. Docker-
	// executor игнорирует.
	JobTTL time.Duration
}

// Типы событий executor'а.
const (
	ExecEventStarted  = "started"
	ExecEventFinished = "finished"
	// ExecEventMetrics — семпл потребления живой попытки; пик пишется в БД
	// по мере выполнения, чтобы не потеряться при смерти таска/OOM.
	ExecEventMetrics = "metrics"
)

// ExecEvent — событие жизненного цикла попытки от executor'а.
type ExecEvent struct {
	Ref  AttemptRef
	Type string
	Exit *ExitInfo // для finished
	// PeakMemoryBytes — для metrics: текущий пик памяти попытки.
	PeakMemoryBytes *int64
}
