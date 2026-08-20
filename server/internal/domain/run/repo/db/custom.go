package db

import (
	"context"
	"fmt"

	sq "github.com/Masterminds/squirrel"

	"github.com/rendau/loom/server/internal/domain/run/model"
)

var allowedSortFields = map[string]string{
	"id":          "id",
	"dag_name":    "dag_name",
	"status":      "status",
	"created_at":  "created_at",
	"finished_at": "finished_at",
}

func (r *Repo) getConditions(pars *model.ListReq) (map[string]any, map[string][]any) {
	conditions := make(map[string]any, 3)
	conditionExps := make(map[string][]any, 3)

	if pars == nil {
		return conditions, conditionExps
	}

	if pars.DagName != nil {
		conditions["dag_name"] = *pars.DagName
	}
	if pars.Status != nil {
		conditions["status"] = *pars.Status
	}

	return conditions, conditionExps
}

// PromoteTaskInstances переводит таски рана из fromStatus в toStatus.
// Для queued проставляется queued_at, для терминальных — finished_at.
func (r *Repo) PromoteTaskInstances(ctx context.Context, runId string, tasks []string, fromStatus, toStatus string) error {
	if len(tasks) == 0 {
		return nil
	}

	qb := r.QB.Update("task_instance").
		Set("status", toStatus).
		Where(sq.Eq{"run_id": runId, "task": tasks, "status": fromStatus})

	if toStatus == model.TaskStatusQueued {
		qb = qb.Set("queued_at", sq.Expr("now()"))
	}
	if model.TaskStatusTerminal(toStatus) {
		qb = qb.Set("finished_at", sq.Expr("now()"))
	}

	query, args, err := qb.ToSql()
	if err != nil {
		return fmt.Errorf("PromoteTaskInstances build query: %w", err)
	}
	if _, err = r.TxM.GetConnection(ctx).Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("PromoteTaskInstances: %w", err)
	}
	return nil
}

// ClaimQueuedTasks забирает из очереди до limit queued-тасков (FOR UPDATE
// SKIP LOCKED — безопасно при нескольких воркерах), переводя их в starting
// с инкрементом номера попытки. Возвращает забранные таски с номером новой
// попытки.
func (r *Repo) ClaimQueuedTasks(ctx context.Context, limit int64) ([]model.ClaimedTask, error) {
	query := `
		UPDATE task_instance AS ti
		SET status = $1, attempt = ti.attempt + 1
		FROM (
			SELECT run_id, task
			FROM task_instance
			WHERE status = $2
			ORDER BY queued_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		) AS q
		WHERE ti.run_id = q.run_id AND ti.task = q.task
		RETURNING ti.run_id, ti.task, ti.attempt`

	rows, err := r.TxM.GetConnection(ctx).Query(ctx, query, model.TaskStatusStarting, model.TaskStatusQueued, limit)
	if err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks: %w", err)
	}
	defer rows.Close()

	result := make([]model.ClaimedTask, 0, limit)
	for rows.Next() {
		var c model.ClaimedTask
		if err = rows.Scan(&c.RunId, &c.Task, &c.Attempt); err != nil {
			return nil, fmt.Errorf("ClaimQueuedTasks scan: %w", err)
		}
		result = append(result, c)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks rows: %w", err)
	}
	return result, nil
}

// MarkAttemptRunning фиксирует старт попытки (pod поднялся): attempt и его
// task instance переходят starting → running. Возвращает false, если
// попытка уже не в starting (дубль события — no-op).
func (r *Repo) MarkAttemptRunning(ctx context.Context, ref model.AttemptRef) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE attempt SET status = $1, started_at = now()
		WHERE run_id = $2 AND task = $3 AND attempt = $4 AND status = $5`,
		model.AttemptStatusRunning, ref.RunId, ref.Task, ref.Attempt, model.AttemptStatusStarting)
	if err != nil {
		return false, fmt.Errorf("MarkAttemptRunning attempt: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	_, err = r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE task_instance SET status = $1, started_at = now()
		WHERE run_id = $2 AND task = $3 AND attempt = $4 AND status = $5`,
		model.TaskStatusRunning, ref.RunId, ref.Task, ref.Attempt, model.TaskStatusStarting)
	if err != nil {
		return false, fmt.Errorf("MarkAttemptRunning task_instance: %w", err)
	}
	return true, nil
}

// FinalizeAttempt переводит попытку в терминальный статус с exit-информацией;
// task instance следует за ней. Идемпотентен: если попытка уже терминальна,
// возвращает false и ничего не меняет (страховочные вызовы, дубли событий).
func (r *Repo) FinalizeAttempt(ctx context.Context, ref model.AttemptRef, exit model.ExitInfo) (bool, error) {
	attemptStatus := model.AttemptStatusFailed
	taskStatus := model.TaskStatusFailed
	if exit.Success {
		attemptStatus = model.AttemptStatusSuccess
		taskStatus = model.TaskStatusSuccess
	}

	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE attempt
		SET status = $1, finished_at = now(), exit_code = $2, exit_reason = $3
		WHERE run_id = $4 AND task = $5 AND attempt = $6 AND status = ANY($7)`,
		attemptStatus, exit.ExitCode, exit.Reason, ref.RunId, ref.Task, ref.Attempt,
		[]string{model.AttemptStatusStarting, model.AttemptStatusRunning})
	if err != nil {
		return false, fmt.Errorf("FinalizeAttempt attempt: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	_, err = r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE task_instance SET status = $1, finished_at = now()
		WHERE run_id = $2 AND task = $3 AND attempt = $4 AND status = ANY($5)`,
		taskStatus, ref.RunId, ref.Task, ref.Attempt,
		[]string{model.TaskStatusStarting, model.TaskStatusRunning})
	if err != nil {
		return false, fmt.Errorf("FinalizeAttempt task_instance: %w", err)
	}
	return true, nil
}

// FinishRun закрывает ран терминальным статусом; идемпотентен.
func (r *Repo) FinishRun(ctx context.Context, runId, status string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE run SET status = $1, finished_at = now()
		WHERE id = $2 AND status = $3`,
		status, runId, model.RunStatusRunning)
	if err != nil {
		return fmt.Errorf("FinishRun: %w", err)
	}
	return nil
}
