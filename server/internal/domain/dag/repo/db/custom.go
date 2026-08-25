package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rendau/loom/server/internal/domain/dag/model"
)

var allowedSortFields = map[string]string{
	"name":        "name",
	"created_at":  "created_at",
	"modified_at": "modified_at",
}

func (r *Repo) getConditions(pars *model.ListReq) (map[string]any, map[string][]any) {
	conditions := make(map[string]any, 3)
	conditionExps := make(map[string][]any, 3)

	if pars == nil {
		return conditions, conditionExps
	}

	if pars.Paused != nil {
		conditions["paused"] = *pars.Paused
	}
	if pars.AutoUpdate != nil {
		conditions["auto_update"] = *pars.AutoUpdate
	}

	return conditions, conditionExps
}

// ListLastRuns — последние perDag ранов каждого из дагов (новые первыми):
// статус-стрип списка дагов в админке.
func (r *Repo) ListLastRuns(ctx context.Context, dagNames []string, perDag int) (map[string][]model.LastRun, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT dag_name, id, status FROM (
			SELECT dag_name, id, status, created_at,
				row_number() OVER (PARTITION BY dag_name ORDER BY created_at DESC) AS rn
			FROM run WHERE dag_name = ANY($1)
		) t WHERE rn <= $2
		ORDER BY dag_name, created_at DESC`, dagNames, perDag)
	if err != nil {
		return nil, fmt.Errorf("ListLastRuns: %w", err)
	}
	defer rows.Close()

	result := map[string][]model.LastRun{}
	for rows.Next() {
		var dagName string
		var lr model.LastRun
		if err = rows.Scan(&dagName, &lr.RunId, &lr.Status); err != nil {
			return nil, fmt.Errorf("ListLastRuns scan: %w", err)
		}
		result[dagName] = append(result[dagName], lr)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListLastRuns rows: %w", err)
	}
	return result, nil
}

// SetNextRun выставляет next_run_at дага; nil сбрасывает в null (даг без
// расписания).
func (r *Repo) SetNextRun(ctx context.Context, name string, t *time.Time) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx,
		`UPDATE dag SET next_run_at = $1 WHERE name = $2`, t, name)
	if err != nil {
		return fmt.Errorf("SetNextRun: %w", err)
	}
	return nil
}

// ListDueNames возвращает имена дагов с расписанием, чей next_run_at
// наступил или ещё не инициализирован.
func (r *Repo) ListDueNames(ctx context.Context) ([]string, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT name FROM dag
		WHERE schedule <> '' AND NOT paused
		  AND (next_run_at IS NULL OR next_run_at <= now())
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("ListDueNames: %w", err)
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ListDueNames scan: %w", err)
		}
		result = append(result, name)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListDueNames rows: %w", err)
	}
	return result, nil
}

// AdvanceNextRun — compare-and-swap next_run_at: двигает вперёд только если
// значение не изменилось с момента выборки (гонка нескольких инстансов
// control plane; IS NOT DISTINCT FROM корректно сравнивает и null). Пауза и
// снятое расписание тоже отменяют сдвиг.
func (r *Repo) AdvanceNextRun(ctx context.Context, name string, from, to time.Time) (bool, error) {
	var fromV *time.Time
	if !from.IsZero() {
		fromV = &from
	}

	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE dag SET next_run_at = $1
		WHERE name = $2 AND schedule <> '' AND NOT paused
		  AND next_run_at IS NOT DISTINCT FROM $3`,
		to, name, fromV)
	if err != nil {
		return false, fmt.Errorf("AdvanceNextRun: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ── task_resources: оверрайды ресурсов тасков из админки ─────────────────

// SetTaskResources создаёт или перезаписывает оверрайд ресурсов таска.
func (r *Repo) SetTaskResources(ctx context.Context, dagName, task string, res model.TaskResources) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO task_resources (dag_name, task, cpu_request, cpu_limit, memory_request, memory_limit)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (dag_name, task) DO UPDATE SET
			cpu_request = excluded.cpu_request,
			cpu_limit = excluded.cpu_limit,
			memory_request = excluded.memory_request,
			memory_limit = excluded.memory_limit,
			modified_at = now()`,
		dagName, task, res.CPURequest, res.CPULimit, res.MemoryRequest, res.MemoryLimit)
	if err != nil {
		return fmt.Errorf("SetTaskResources: %w", err)
	}
	return nil
}

func (r *Repo) DeleteTaskResources(ctx context.Context, dagName, task string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx,
		`DELETE FROM task_resources WHERE dag_name = $1 AND task = $2`, dagName, task)
	if err != nil {
		return false, fmt.Errorf("DeleteTaskResources: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) ListTaskResources(ctx context.Context, dagName string) ([]*model.TaskResourcesEntry, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT task, cpu_request, cpu_limit, memory_request, memory_limit, modified_at
		FROM task_resources WHERE dag_name = $1 ORDER BY task`, dagName)
	if err != nil {
		return nil, fmt.Errorf("ListTaskResources: %w", err)
	}
	defer rows.Close()

	var result []*model.TaskResourcesEntry
	for rows.Next() {
		var e model.TaskResourcesEntry
		if err = rows.Scan(&e.Task, &e.Res.CPURequest, &e.Res.CPULimit,
			&e.Res.MemoryRequest, &e.Res.MemoryLimit, &e.ModifiedAt); err != nil {
			return nil, fmt.Errorf("ListTaskResources scan: %w", err)
		}
		result = append(result, &e)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListTaskResources rows: %w", err)
	}
	return result, nil
}

// GetTaskResources — оверрайд одного таска; nil — оверрайда нет.
func (r *Repo) GetTaskResources(ctx context.Context, dagName, task string) (*model.TaskResources, error) {
	var res model.TaskResources
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT cpu_request, cpu_limit, memory_request, memory_limit
		FROM task_resources WHERE dag_name = $1 AND task = $2`, dagName, task).
		Scan(&res.CPURequest, &res.CPULimit, &res.MemoryRequest, &res.MemoryLimit)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("GetTaskResources: %w", err)
	}
	return &res, nil
}
