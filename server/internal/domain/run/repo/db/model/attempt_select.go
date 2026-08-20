package model

import (
	"database/sql"
	"time"

	domainModel "github.com/rendau/loom/server/internal/domain/run/model"
)

type AttemptSelect struct {
	RunId      string
	Task       string
	Attempt    int32
	Status     string
	CreatedAt  time.Time
	StartedAt  sql.NullTime
	FinishedAt sql.NullTime
	ExitCode   sql.NullInt32
	ExitReason string
}

func (m *AttemptSelect) ListColumnMap() map[string]any {
	return map[string]any{
		"run_id":      &m.RunId,
		"task":        &m.Task,
		"attempt":     &m.Attempt,
		"status":      &m.Status,
		"created_at":  &m.CreatedAt,
		"started_at":  &m.StartedAt,
		"finished_at": &m.FinishedAt,
		"exit_code":   &m.ExitCode,
		"exit_reason": &m.ExitReason,
	}
}

func (m *AttemptSelect) PKColumnMap() map[string]any {
	return map[string]any{"run_id": m.RunId, "task": m.Task, "attempt": m.Attempt}
}

func (m *AttemptSelect) DefaultSortColumns() []string {
	return []string{"task", "attempt"}
}

// DTO

func EncodeAttemptSelect(v *AttemptSelect, _ int) *domainModel.Attempt {
	result := &domainModel.Attempt{
		RunId:      v.RunId,
		Task:       v.Task,
		Attempt:    v.Attempt,
		Status:     v.Status,
		CreatedAt:  v.CreatedAt,
		StartedAt:  v.StartedAt.Time,
		FinishedAt: v.FinishedAt.Time,
		ExitReason: v.ExitReason,
	}
	if v.ExitCode.Valid {
		result.ExitCode = new(v.ExitCode.Int32)
	}
	return result
}
