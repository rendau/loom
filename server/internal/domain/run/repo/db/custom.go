package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/run/model"
)

var allowedSortFields = map[string]string{
	"id":           "id",
	"dag_name":     "dag_name",
	"project_name": "project_name",
	"status":       "status",
	"created_at":   "created_at",
	"finished_at":  "finished_at",
}

func (r *Repo) getConditions(pars *model.ListReq) (map[string]any, map[string][]any) {
	conditions := make(map[string]any, 3)
	conditionExps := make(map[string][]any, 3)

	if pars == nil {
		return conditions, conditionExps
	}

	if pars.Dag != nil {
		conditions["project_name"] = pars.Dag.Project
		conditions["dag_name"] = pars.Dag.Name
	}
	if pars.Project != nil {
		conditions["project_name"] = *pars.Project
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
// свободных слотов пулов, переводя их в starting с инкрементом
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
// дубли событий). startedAt — время старта попытки (nil — не стартовала,
// например launch_failed) для метрики длительности.
func (r *Repo) FinalizeAttempt(ctx context.Context, ref model.AttemptRef, exit model.ExitInfo, retryAt *time.Time) (bool, *time.Time, error) {
	attemptStatus := model.AttemptStatusFailed
	taskStatus := model.TaskStatusFailed
	if exit.Success {
		attemptStatus = model.AttemptStatusSuccess
		taskStatus = model.TaskStatusSuccess
		retryAt = nil
	}
	// остановка рана: попытка неуспешна, но таск не «упал» и ретрая не ждёт
	if exit.Canceled {
		taskStatus = model.TaskStatusCanceled
		retryAt = nil
	}
	if retryAt != nil {
		taskStatus = model.TaskStatusUpForRetry
	}

	var startedAt *time.Time
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		UPDATE attempt
		SET status = $1, finished_at = now(), exit_code = $2, exit_reason = $3
		WHERE run_id = $4 AND task = $5 AND attempt = $6 AND status = ANY($7)
		RETURNING started_at`,
		attemptStatus, exit.ExitCode, exit.Reason, ref.RunId, ref.Task, ref.Attempt,
		[]string{model.AttemptStatusStarting, model.AttemptStatusRunning}).Scan(&startedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("FinalizeAttempt attempt: %w", err)
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
		return false, nil, fmt.Errorf("FinalizeAttempt task_instance: %w", err)
	}
	return true, startedAt, nil
}

// SetAttemptPeakMemory поднимает пик памяти попытки до значения семпла:
// greatest делает запись идемпотентной, поздний семпл после финализации
// значение не занизит.
func (r *Repo) SetAttemptPeakMemory(ctx context.Context, ref model.AttemptRef, peakBytes int64) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE attempt
		SET peak_memory_bytes = greatest(coalesce(peak_memory_bytes, 0), $4)
		WHERE run_id = $1 AND task = $2 AND attempt = $3`,
		ref.RunId, ref.Task, ref.Attempt, peakBytes)
	if err != nil {
		return fmt.Errorf("SetAttemptPeakMemory: %w", err)
	}
	return nil
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
		model.RunStatusRunning, runId,
		[]string{model.RunStatusSuccess, model.RunStatusFailed, model.RunStatusCanceled})
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
		[]string{model.TaskStatusSuccess, model.TaskStatusFailed, model.TaskStatusCanceled})
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

// CancelRun останавливает выполняющийся ран: ран и не начавшие исполняться
// таски (pending | queued | up_for_retry) получают canceled, а живые попытки
// (starting | running) возвращаются вызывающему — их убивает и финализирует
// планировщик. Идемпотентен: applied=false — ран уже не выполняется, ничего
// не изменено. Вызывать в транзакции: замок на строках очереди сериализует
// остановку с claim'ом планировщика (таск либо уже не попадёт в запуск, либо
// вернётся живой попыткой).
func (r *Repo) CancelRun(ctx context.Context, runId string) (bool, []model.AttemptRef, error) {
	con := r.TxM.GetConnection(ctx)

	tag, err := con.Exec(ctx, `
		UPDATE run SET status = $1, finished_at = now()
		WHERE id = $2 AND status = $3`,
		model.RunStatusCanceled, runId, model.RunStatusRunning)
	if err != nil {
		return false, nil, fmt.Errorf("CancelRun run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return false, nil, nil
	}

	_, err = con.Exec(ctx, `
		UPDATE task_instance
		SET status = $1, retry_at = NULL, finished_at = now()
		WHERE run_id = $2 AND status = ANY($3)`,
		model.TaskStatusCanceled, runId,
		[]string{model.TaskStatusPending, model.TaskStatusQueued, model.TaskStatusUpForRetry})
	if err != nil {
		return false, nil, fmt.Errorf("CancelRun task_instance: %w", err)
	}

	// живые попытки читаем после апдейта очереди: таск, забранный на запуск
	// конкурентным claim'ом, к этому моменту уже виден как starting
	rows, err := con.Query(ctx, `
		SELECT task, attempt FROM task_instance
		WHERE run_id = $1 AND status = ANY($2)`,
		runId, []string{model.TaskStatusStarting, model.TaskStatusRunning})
	if err != nil {
		return false, nil, fmt.Errorf("CancelRun live: %w", err)
	}
	defer rows.Close()

	var live []model.AttemptRef
	for rows.Next() {
		ref := model.AttemptRef{RunId: runId}
		if err = rows.Scan(&ref.Task, &ref.Attempt); err != nil {
			return false, nil, fmt.Errorf("CancelRun live scan: %w", err)
		}
		live = append(live, ref)
	}
	if err = rows.Err(); err != nil {
		return false, nil, fmt.Errorf("CancelRun live rows: %w", err)
	}
	return true, live, nil
}

// CountRunsByStatus — счётчики ранов по статусам (фильтры-чипы админки);
// ref != nil — только раны этого дага.
func (r *Repo) CountRunsByStatus(ctx context.Context, ref *dagModel.Ref) (map[string]int64, error) {
	query := `SELECT status, count(*) FROM run`
	var args []any
	if ref != nil {
		query += ` WHERE project_name = $1 AND dag_name = $2`
		args = append(args, ref.Project, ref.Name)
	}
	query += ` GROUP BY status`

	rows, err := r.TxM.GetConnection(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("CountRunsByStatus: %w", err)
	}
	defer rows.Close()

	result := map[string]int64{}
	for rows.Next() {
		var status string
		var count int64
		if err = rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("CountRunsByStatus scan: %w", err)
		}
		result[status] = count
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("CountRunsByStatus rows: %w", err)
	}
	return result, nil
}

// UpsertRunEnv сохраняет снапшот env-резолва рана (batch upsert): повторный
// launch (ретрай) обновляет записи фактической инъекцией.
func (r *Repo) UpsertRunEnv(ctx context.Context, runId string, entries []model.RunEnv) error {
	b := &pgx.Batch{}
	for _, e := range entries {
		b.Queue(`
			INSERT INTO run_env (run_id, env, kind, name, scope_kind, scope_name, value)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (run_id, kind, env)
			DO UPDATE SET name = excluded.name, scope_kind = excluded.scope_kind,
				scope_name = excluded.scope_name,
				value = excluded.value, resolved_at = now()`,
			runId, e.Env, e.Kind, e.Name, e.Scope.Kind(), e.Scope.String(), e.Value)
	}
	if err := r.TxM.GetConnection(ctx).SendBatch(ctx, b).Close(); err != nil {
		return fmt.Errorf("UpsertRunEnv: %w", err)
	}
	return nil
}

func (r *Repo) ListRunEnv(ctx context.Context, runId string) ([]model.RunEnv, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT env, kind, name, scope_kind, scope_name, value, resolved_at FROM run_env
		WHERE run_id = $1 ORDER BY env, kind`, runId)
	if err != nil {
		return nil, fmt.Errorf("ListRunEnv: %w", err)
	}
	defer rows.Close()

	var result []model.RunEnv
	for rows.Next() {
		var e model.RunEnv
		var scopeKind, scopeName string
		if err = rows.Scan(&e.Env, &e.Kind, &e.Name, &scopeKind, &scopeName,
			&e.Value, &e.ResolvedAt); err != nil {
			return nil, fmt.Errorf("ListRunEnv scan: %w", err)
		}
		e.Scope = decodeScope(scopeKind, scopeName)
		result = append(result, e)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListRunEnv rows: %w", err)
	}
	return result, nil
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

// FinishRun закрывает ран терминальным статусом; идемпотентен: false — ран
// уже был завершён (гонка инстансов планировщика).
func (r *Repo) FinishRun(ctx context.Context, runId, status string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE run SET status = $1, finished_at = now()
		WHERE id = $2 AND status = $3`,
		status, runId, model.RunStatusRunning)
	if err != nil {
		return false, fmt.Errorf("FinishRun: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// CountActiveTaskInstances — количество тасков в каждом нетерминальном
// статусе (глубина очереди планировщика для метрик).
func (r *Repo) CountActiveTaskInstances(ctx context.Context) (map[string]int64, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT status, count(*) FROM task_instance
		WHERE status = ANY($1) GROUP BY status`,
		[]string{model.TaskStatusPending, model.TaskStatusQueued, model.TaskStatusStarting,
			model.TaskStatusRunning, model.TaskStatusUpForRetry})
	if err != nil {
		return nil, fmt.Errorf("CountActiveTaskInstances: %w", err)
	}
	defer rows.Close()

	result := map[string]int64{}
	for rows.Next() {
		var status string
		var count int64
		if err = rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("CountActiveTaskInstances scan: %w", err)
		}
		result[status] = count
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("CountActiveTaskInstances rows: %w", err)
	}
	return result, nil
}

// ListPoolUsage — слоты и занятость (starting+running) каждого пула.
func (r *Repo) ListPoolUsage(ctx context.Context) ([]model.PoolUsage, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT p.name, p.slots, count(ti.run_id)
		FROM pool p
		LEFT JOIN task_instance ti ON ti.pool = p.name AND ti.status = ANY($1)
		GROUP BY p.name, p.slots`,
		[]string{model.TaskStatusStarting, model.TaskStatusRunning})
	if err != nil {
		return nil, fmt.Errorf("ListPoolUsage: %w", err)
	}
	defer rows.Close()

	var result []model.PoolUsage
	for rows.Next() {
		var u model.PoolUsage
		if err = rows.Scan(&u.Pool, &u.Slots, &u.Busy); err != nil {
			return nil, fmt.Errorf("ListPoolUsage scan: %w", err)
		}
		result = append(result, u)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListPoolUsage rows: %w", err)
	}
	return result, nil
}

// ListRetentionDags возвращает даги, у которых есть завершённые раны —
// скоупы retention-прохода (включая даги, уже удалённые из dag: их раны
// тоже подлежат очистке, настройки резолвятся до глобальных).
func (r *Repo) ListRetentionDags(ctx context.Context) ([]dagModel.Ref, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT DISTINCT project_name, dag_name FROM run WHERE finished_at IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("ListRetentionDags: %w", err)
	}
	defer rows.Close()

	var result []dagModel.Ref
	for rows.Next() {
		var ref dagModel.Ref
		if err = rows.Scan(&ref.Project, &ref.Name); err != nil {
			return nil, fmt.Errorf("ListRetentionDags scan: %w", err)
		}
		result = append(result, ref)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListRetentionDags rows: %w", err)
	}
	return result, nil
}

// ListExpiredRuns возвращает id завершённых ранов дага, нарушающих любой
// из retention-лимитов (старые первыми): finished_at раньше before
// (nil — по времени не чистить) либо за пределами keepLast последних
// завершённых (0 — количество не ограничено).
func (r *Repo) ListExpiredRuns(ctx context.Context, ref dagModel.Ref, before *time.Time,
	keepLast, limit int64,
) ([]string, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT id FROM (
			SELECT id, finished_at,
				row_number() OVER (ORDER BY finished_at DESC) AS rn
			FROM run
			WHERE project_name = $1 AND dag_name = $2 AND finished_at IS NOT NULL
		) t
		WHERE ($3::timestamptz IS NOT NULL AND finished_at < $3)
			OR ($4::bigint > 0 AND rn > $4)
		ORDER BY finished_at
		LIMIT $5`, ref.Project, ref.Name, before, keepLast, limit)
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

// decodeScope восстанавливает скоуп из снапшота run_env: у дага имя
// хранится в форме «проект/даг».
func decodeScope(kind, name string) commonModel.Scope {
	switch kind {
	case "dag":
		if ref, ok := dagModel.ParseRef(name); ok {
			return ref.Scope()
		}
		return commonModel.GlobalScope()
	case "project":
		return commonModel.ProjectScope(name)
	default:
		return commonModel.GlobalScope()
	}
}
