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
)

// Статусы task instance.
//
//	pending → queued → starting → running → success | failed
//	pending → upstream_failed (зависимость упала)
//	starting/running → up_for_retry → queued (остались ретраи, ждём backoff)
const (
	TaskStatusPending        = "pending"
	TaskStatusQueued         = "queued"
	TaskStatusStarting       = "starting"
	TaskStatusRunning        = "running"
	TaskStatusUpForRetry     = "up_for_retry"
	TaskStatusSuccess        = "success"
	TaskStatusFailed         = "failed"
	TaskStatusUpstreamFailed = "upstream_failed"
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

// MaxParamsSize — лимит params рана (решение №23): это конфиг, а не данные;
// данные текут артефактами.
const MaxParamsSize = 64 * 1024

// MaxValueSize — лимит значения таска (решение №25): значения — для мелочи
// вроде счётчиков и id, данные текут артефактами.
const MaxValueSize = 64 * 1024

// TaskStatusTerminal — терминален ли статус task instance.
func TaskStatusTerminal(status string) bool {
	switch status {
	case TaskStatusSuccess, TaskStatusFailed, TaskStatusUpstreamFailed:
		return true
	}
	return false
}

// Main — ран дага. Manifest — снапшот манифеста на момент триггера
// (ран не зависит от последующих перерегистраций дага). Params — параметры
// рана (raw JSON-объект, nil — без параметров), LogicalDate — «дата данных»
// (решение №23).
type Main struct {
	Id          string
	DagName     string
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

	DagName *string
	Status  *string
}

// TaskInstance — таск внутри рана; attempt — номер текущей (последней)
// попытки, 0 — ещё не стартовала. Pool/Priority — денормализация из
// манифеста для claim-запроса очереди (решение №26).
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
}

// AttemptRef — идентификация попытки.
type AttemptRef struct {
	RunId   string
	Task    string
	Attempt int32
}

// TaskValue — мелкое значение таска (аналог XCom, решение №25): скоуп
// (run, task, key), ретрай перезаписывает.
type TaskValue struct {
	RunId      string
	Task       string
	Key        string
	Value      []byte // raw JSON
	ModifiedAt time.Time
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
}

// LaunchSpec — параметры запуска попытки executor'ом: образ рана (пиннутый
// digest), env-контракт LOOM_* и ресурсы контейнера из манифеста.
type LaunchSpec struct {
	Ref       AttemptRef
	Image     string
	Env       map[string]string
	Resources *dagModel.TaskResources
}

// Типы событий executor'а.
const (
	ExecEventStarted  = "started"
	ExecEventFinished = "finished"
)

// ExecEvent — событие жизненного цикла попытки от executor'а.
type ExecEvent struct {
	Ref  AttemptRef
	Type string
	Exit *ExitInfo // для finished
}
