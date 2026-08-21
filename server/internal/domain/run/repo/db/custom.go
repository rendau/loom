package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

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

// claimCandidateLimit — сколько queued-кандидатов рассматривает один claim:
// запас на таски полных пулов, которые придётся пропустить.
const claimCandidateLimit = 500

// ClaimQueuedTasks забирает из очереди до limit queued-тасков с учётом
// свободных слотов пулов (решение №26), переводя их в starting с инкрементом
// номера попытки. Пулы лочатся FOR UPDATE — claim'ы конкурентных инстансов
// сериализуются и не перебирают слоты; кандидаты — приоритетные первыми
// (внутри приоритета — старейшие), под FOR UPDATE SKIP LOCKED. Вызывать в
// транзакции. Возвращает забранные таски с номером новой попытки.
func (r *Repo) ClaimQueuedTasks(ctx context.Context, limit int64) ([]model.ClaimedTask, error) {
	con := r.TxM.GetConnection(ctx)

	// слоты пулов держим под замком до конца транзакции (FOR UPDATE нельзя
	// комбинировать с GROUP BY — занятость считаем отдельным запросом)
	free := map[string]int64{}
	rows, err := con.Query(ctx, `SELECT name, slots FROM pool FOR UPDATE`)
	if err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks pools: %w", err)
	}
	for rows.Next() {
		var name string
		var slots int64
		if err = rows.Scan(&name, &slots); err != nil {
			rows.Close()
			return nil, fmt.Errorf("ClaimQueuedTasks pools scan: %w", err)
		}
		free[name] = slots
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks pools rows: %w", err)
	}

	rows, err = con.Query(ctx, `
		SELECT pool, count(*) FROM task_instance
		WHERE status = ANY($1) GROUP BY pool`,
		[]string{model.TaskStatusStarting, model.TaskStatusRunning})
	if err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks occupancy: %w", err)
	}
	for rows.Next() {
		var pool string
		var busy int64
		if err = rows.Scan(&pool, &busy); err != nil {
			rows.Close()
			return nil, fmt.Errorf("ClaimQueuedTasks occupancy scan: %w", err)
		}
		free[pool] -= busy
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks occupancy rows: %w", err)
	}

	// кандидаты: приоритетные первыми; таски полных пулов пропускаем
	rows, err = con.Query(ctx, `
		SELECT run_id, task, pool
		FROM task_instance
		WHERE status = $1
		ORDER BY priority DESC, queued_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED`,
		model.TaskStatusQueued, claimCandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks candidates: %w", err)
	}
	var runIds, tasks []string
	for rows.Next() {
		var runId, task, pool string
		if err = rows.Scan(&runId, &task, &pool); err != nil {
			rows.Close()
			return nil, fmt.Errorf("ClaimQueuedTasks candidates scan: %w", err)
		}
		// неизвестный пул (запись удалена руками из БД) — слотов нет
		if int64(len(runIds)) >= limit || free[pool] <= 0 {
			continue
		}
		free[pool]--
		runIds = append(runIds, runId)
		tasks = append(tasks, task)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks candidates rows: %w", err)
	}
	if len(runIds) == 0 {
		return nil, nil
	}

	rows, err = con.Query(ctx, `
		UPDATE task_instance AS ti
		SET status = $1, attempt = ti.attempt + 1
		FROM unnest($2::text[], $3::text[]) AS q(run_id, task)
		WHERE ti.run_id = q.run_id AND ti.task = q.task
		RETURNING ti.run_id, ti.task, ti.attempt`,
		model.TaskStatusStarting, runIds, tasks)
	if err != nil {
		return nil, fmt.Errorf("ClaimQueuedTasks: %w", err)
	}
	defer rows.Close()

	result := make([]model.ClaimedTask, 0, len(runIds))
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
// task instance следует за ней: success/failed, а при неуспехе с retryAt —
// up_for_retry с отложенным возвратом в очередь. Идемпотентен: если попытка
// уже терминальна, возвращает false и ничего не меняет (страховочные вызовы,
// дубли событий).
func (r *Repo) FinalizeAttempt(ctx context.Context, ref model.AttemptRef, exit model.ExitInfo, retryAt *time.Time) (bool, error) {
	attemptStatus := model.AttemptStatusFailed
	taskStatus := model.TaskStatusFailed
	if exit.Success {
		attemptStatus = model.AttemptStatusSuccess
		taskStatus = model.TaskStatusSuccess
		retryAt = nil
	}
	if retryAt != nil {
		taskStatus = model.TaskStatusUpForRetry
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

	// finished_at ставится только терминальному статусу: up_for_retry — таск
	// ещё не завершён, он ждёт следующую попытку
	_, err = r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE task_instance
		SET status = $1, retry_at = $2,
		    finished_at = CASE WHEN $2::timestamptz IS NULL THEN now() END
		WHERE run_id = $3 AND task = $4 AND attempt = $5 AND status = ANY($6)`,
		taskStatus, retryAt, ref.RunId, ref.Task, ref.Attempt,
		[]string{model.TaskStatusStarting, model.TaskStatusRunning})
	if err != nil {
		return false, fmt.Errorf("FinalizeAttempt task_instance: %w", err)
	}
	return true, nil
}

// ListStaleAttempts возвращает незавершённые попытки, созданные раньше
// olderThan — кандидатов на зомби-детект.
func (r *Repo) ListStaleAttempts(ctx context.Context, olderThan time.Time) ([]model.StaleAttempt, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT run_id, task, attempt, status FROM attempt
		WHERE status = ANY($1) AND created_at < $2`,
		[]string{model.AttemptStatusStarting, model.AttemptStatusRunning}, olderThan)
	if err != nil {
		return nil, fmt.Errorf("ListStaleAttempts: %w", err)
	}
	defer rows.Close()

	var result []model.StaleAttempt
	for rows.Next() {
		var a model.StaleAttempt
		if err = rows.Scan(&a.Ref.RunId, &a.Ref.Task, &a.Ref.Attempt, &a.Status); err != nil {
			return nil, fmt.Errorf("ListStaleAttempts scan: %w", err)
		}
		result = append(result, a)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListStaleAttempts rows: %w", err)
	}
	return result, nil
}

// PromoteRetries возвращает в очередь up_for_retry-таски, чей backoff истёк.
func (r *Repo) PromoteRetries(ctx context.Context) (int64, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE task_instance
		SET status = $1, queued_at = now(), retry_at = NULL
		WHERE status = $2 AND retry_at <= now()`,
		model.TaskStatusQueued, model.TaskStatusUpForRetry)
	if err != nil {
		return 0, fmt.Errorf("PromoteRetries: %w", err)
	}
	return tag.RowsAffected(), nil
}

// RetryTaskSubgraph возвращает таск завершённого рана в очередь, сбрасывает
// его downstream-подграф в pending и реактивирует ран (running). Guarded:
// возвращает false без изменений, если ран уже не терминален или таск не в
// ретраебельном статусе (гонка конкурентных вызовов) — вызывать в
// транзакции, false у вызывающего должен откатить её целиком.
func (r *Repo) RetryTaskSubgraph(ctx context.Context, runId, task string, downstream []string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE run SET status = $1, finished_at = NULL
		WHERE id = $2 AND status = ANY($3)`,
		model.RunStatusRunning, runId, []string{model.RunStatusSuccess, model.RunStatusFailed})
	if err != nil {
		return false, fmt.Errorf("RetryTaskSubgraph run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	tag, err = r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE task_instance
		SET status = $1, queued_at = now(), started_at = NULL, retry_at = NULL, finished_at = NULL
		WHERE run_id = $2 AND task = $3 AND status = ANY($4)`,
		model.TaskStatusQueued, runId, task,
		[]string{model.TaskStatusSuccess, model.TaskStatusFailed})
	if err != nil {
		return false, fmt.Errorf("RetryTaskSubgraph task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}

	if len(downstream) > 0 {
		_, err = r.TxM.GetConnection(ctx).Exec(ctx, `
			UPDATE task_instance
			SET status = $1, queued_at = NULL, started_at = NULL, retry_at = NULL, finished_at = NULL
			WHERE run_id = $2 AND task = ANY($3)`,
			model.TaskStatusPending, runId, downstream)
		if err != nil {
			return false, fmt.Errorf("RetryTaskSubgraph downstream: %w", err)
		}
	}

	return true, nil
}

// UpsertTaskValue сохраняет значение таска; повторный пуш по тому же ключу
// перезаписывает (ретрай публикует значения заново).
func (r *Repo) UpsertTaskValue(ctx context.Context, v *model.TaskValue) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO run_value (run_id, task, key, value) VALUES ($1, $2, $3, $4)
		ON CONFLICT (run_id, task, key)
		DO UPDATE SET value = excluded.value, modified_at = now()`,
		v.RunId, v.Task, v.Key, v.Value)
	if err != nil {
		return fmt.Errorf("UpsertTaskValue: %w", err)
	}
	return nil
}

func (r *Repo) GetTaskValue(ctx context.Context, runId, task, key string) (*model.TaskValue, bool, error) {
	v := model.TaskValue{RunId: runId, Task: task, Key: key}
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT value, modified_at FROM run_value
		WHERE run_id = $1 AND task = $2 AND key = $3`,
		runId, task, key).Scan(&v.Value, &v.ModifiedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("GetTaskValue: %w", err)
	}
	return &v, true, nil
}

func (r *Repo) ListTaskValues(ctx context.Context, runId string) ([]*model.TaskValue, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT task, key, value, modified_at FROM run_value
		WHERE run_id = $1 ORDER BY task, key`, runId)
	if err != nil {
		return nil, fmt.Errorf("ListTaskValues: %w", err)
	}
	defer rows.Close()

	var result []*model.TaskValue
	for rows.Next() {
		v := model.TaskValue{RunId: runId}
		if err = rows.Scan(&v.Task, &v.Key, &v.Value, &v.ModifiedAt); err != nil {
			return nil, fmt.Errorf("ListTaskValues scan: %w", err)
		}
		result = append(result, &v)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListTaskValues rows: %w", err)
	}
	return result, nil
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

// ListExpiredRuns возвращает id завершённых ранов с finished_at раньше
// before — кандидатов retention-очистки (старые первыми).
func (r *Repo) ListExpiredRuns(ctx context.Context, before time.Time, limit int64) ([]string, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT id FROM run
		WHERE finished_at IS NOT NULL AND finished_at < $1
		ORDER BY finished_at
		LIMIT $2`, before, limit)
	if err != nil {
		return nil, fmt.Errorf("ListExpiredRuns: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("ListExpiredRuns scan: %w", err)
		}
		result = append(result, id)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListExpiredRuns rows: %w", err)
	}
	return result, nil
}

// DeleteRun удаляет ран из БД; task_instance и attempt уходят каскадом.
// Активный ран не трогаем: ретрай таска мог реактивировать его между
// выборкой retention-кандидатов и удалением.
func (r *Repo) DeleteRun(ctx context.Context, runId string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		DELETE FROM run WHERE id = $1 AND status <> $2`, runId, model.RunStatusRunning)
	if err != nil {
		return fmt.Errorf("DeleteRun: %w", err)
	}
	return nil
}
