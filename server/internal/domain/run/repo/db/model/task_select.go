package model

import (
	"database/sql"

	domainModel "github.com/rendau/loom/server/internal/domain/run/model"
)

type TaskSelect struct {
	RunId      string
	Task       string
	Status     string
	Attempt    int32
	QueuedAt   sql.NullTime
	StartedAt  sql.NullTime
	FinishedAt sql.NullTime
}

func (m *TaskSelect) ListColumnMap() map[string]any {
	return map[string]any{
		"run_id":      &m.RunId,
		"task":        &m.Task,
		"status":      &m.Status,
		"attempt":     &m.Attempt,
		"queued_at":   &m.QueuedAt,
		"started_at":  &m.StartedAt,
		"finished_at": &m.FinishedAt,
	}
}

func (m *TaskSelect) PKColumnMap() map[string]any {
	return map[string]any{"run_id": m.RunId, "task": m.Task}
}

func (m *TaskSelect) DefaultSortColumns() []string {
	return []string{"task"}
}

// DTO

func EncodeTaskSelect(v *TaskSelect, _ int) *domainModel.TaskInstance {
	return &domainModel.TaskInstance{
		RunId:      v.RunId,
		Task:       v.Task,
		Status:     v.Status,
		Attempt:    v.Attempt,
		QueuedAt:   v.QueuedAt.Time,
		StartedAt:  v.StartedAt.Time,
		FinishedAt: v.FinishedAt.Time,
	}
}
