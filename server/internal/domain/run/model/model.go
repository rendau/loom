package model

import (
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
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
const (
	TaskStatusPending        = "pending"
	TaskStatusQueued         = "queued"
	TaskStatusStarting       = "starting"
	TaskStatusRunning        = "running"
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
)

// TaskStatusTerminal — терминален ли статус task instance.
func TaskStatusTerminal(status string) bool {
	switch status {
	case TaskStatusSuccess, TaskStatusFailed, TaskStatusUpstreamFailed:
		return true
	}
	return false
}

// Main — ран дага. Manifest — снапшот манифеста на момент триггера
// (ран не зависит от последующих перерегистраций дага).
type Main struct {
	Id          string
	DagName     string
	Image       string // образ для запуска подов: repo@digest
	ImageDigest string
	Trigger     string
	Status      string
	Manifest    []byte
	CreatedAt   time.Time
	FinishedAt  time.Time // zero — ран ещё идёт
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
// попытки, 0 — ещё не стартовала.
type TaskInstance struct {
	RunId      string
	Task       string
	Status     string
	Attempt    int32
	QueuedAt   time.Time
	StartedAt  time.Time
	FinishedAt time.Time
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

// ClaimedTask — таск, забранный из очереди на запуск.
type ClaimedTask struct {
	RunId   string
	Task    string
	Attempt int32 // номер новой попытки
}

// ExitInfo — результат завершения попытки.
type ExitInfo struct {
	Success  bool
	ExitCode *int32
	Reason   string
	Message  string
}

// LaunchSpec — параметры запуска попытки executor'ом: образ рана (пиннутый
// digest) и env-контракт LOOM_* для контейнера.
type LaunchSpec struct {
	Ref   AttemptRef
	Image string
	Env   map[string]string
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
