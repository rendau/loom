package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/samber/lo"

	"github.com/rendau/loom/server/internal/domain/dag/model"
)

var allowedSortFields = map[string]string{
	"name":         "name",
	"project_name": "project_name",
	"created_at":   "created_at",
	"modified_at":  "modified_at",
}

func (r *Repo) getConditions(pars *model.ListReq) (map[string]any, map[string][]any) {
	conditions := make(map[string]any, 4)
	conditionExps := make(map[string][]any, 4)

	if pars == nil {
		return conditions, conditionExps
	}

	if pars.Project != nil {
		conditions["project_name"] = *pars.Project
	}
	if pars.Template != nil {
		conditions["template"] = *pars.Template
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
func (r *Repo) ListLastRuns(ctx context.Context, refs []model.Ref, perDag int) (map[model.Ref][]model.LastRun, error) {
	projects := lo.Map(refs, func(v model.Ref, _ int) string { return v.Project })
	names := lo.Map(refs, func(v model.Ref, _ int) string { return v.Name })

	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT project_name, dag_name, id, status FROM (
			SELECT project_name, dag_name, id, status, created_at,
				row_number() OVER (PARTITION BY project_name, dag_name
				                   ORDER BY created_at DESC) AS rn
			FROM run
			WHERE (project_name, dag_name) IN (
				SELECT * FROM unnest($1::text[], $2::text[]))
		) t WHERE rn <= $3
		ORDER BY project_name, dag_name, created_at DESC`, projects, names, perDag)
	if err != nil {
		return nil, fmt.Errorf("ListLastRuns: %w", err)
	}
	defer rows.Close()

	result := map[model.Ref][]model.LastRun{}
	for rows.Next() {
		var ref model.Ref
		var lr model.LastRun
		if err = rows.Scan(&ref.Project, &ref.Name, &lr.RunId, &lr.Status); err != nil {
			return nil, fmt.Errorf("ListLastRuns scan: %w", err)
		}
		result[ref] = append(result[ref], lr)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListLastRuns rows: %w", err)
	}
	return result, nil
}

// SetNextRun выставляет next_run_at дага; nil сбрасывает в null (даг без
// расписания).
func (r *Repo) SetNextRun(ctx context.Context, ref model.Ref, t *time.Time) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx,
		`UPDATE dag SET next_run_at = $1 WHERE project_name = $2 AND name = $3`,
		t, ref.Project, ref.Name)
	if err != nil {
		return fmt.Errorf("SetNextRun: %w", err)
	}
	return nil
}

// ListDueRefs возвращает даги с расписанием, чей next_run_at наступил или
// ещё не инициализирован.
func (r *Repo) ListDueRefs(ctx context.Context) ([]model.Ref, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT project_name, name FROM dag
		WHERE schedule <> '' AND NOT paused
		  AND (next_run_at IS NULL OR next_run_at <= now())
		ORDER BY project_name, name`)
	if err != nil {
		return nil, fmt.Errorf("ListDueRefs: %w", err)
	}
	defer rows.Close()

	var result []model.Ref
	for rows.Next() {
		var ref model.Ref
		if err = rows.Scan(&ref.Project, &ref.Name); err != nil {
			return nil, fmt.Errorf("ListDueRefs scan: %w", err)
		}
		result = append(result, ref)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListDueRefs rows: %w", err)
	}
	return result, nil
}

// AdvanceNextRun — compare-and-swap next_run_at: двигает вперёд только если
// значение не изменилось с момента выборки (гонка нескольких инстансов
// control plane; IS NOT DISTINCT FROM корректно сравнивает и null). Пауза и
// снятое расписание тоже отменяют сдвиг.
func (r *Repo) AdvanceNextRun(ctx context.Context, ref model.Ref, from, to time.Time) (bool, error) {
	var fromV *time.Time
	if !from.IsZero() {
		fromV = &from
	}

	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE dag SET next_run_at = $1
		WHERE project_name = $2 AND name = $3 AND schedule <> '' AND NOT paused
		  AND next_run_at IS NOT DISTINCT FROM $4`,
		to, ref.Project, ref.Name, fromV)
	if err != nil {
		return false, fmt.Errorf("AdvanceNextRun: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ── task_resources: оверрайды ресурсов тасков из админки ─────────────────

// SetTaskResources создаёт или перезаписывает оверрайд ресурсов таска.
func (r *Repo) SetTaskResources(ctx context.Context, ref model.Ref, task string, res model.TaskResources) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO task_resources (project_name, dag_name, task,
		                            cpu_request, cpu_limit, memory_request, memory_limit)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (project_name, dag_name, task) DO UPDATE SET
			cpu_request = excluded.cpu_request,
			cpu_limit = excluded.cpu_limit,
			memory_request = excluded.memory_request,
			memory_limit = excluded.memory_limit,
			modified_at = now()`,
		ref.Project, ref.Name, task, res.CPURequest, res.CPULimit, res.MemoryRequest, res.MemoryLimit)
	if err != nil {
		return fmt.Errorf("SetTaskResources: %w", err)
	}
	return nil
}

func (r *Repo) DeleteTaskResources(ctx context.Context, ref model.Ref, task string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		DELETE FROM task_resources
		WHERE project_name = $1 AND dag_name = $2 AND task = $3`, ref.Project, ref.Name, task)
	if err != nil {
		return false, fmt.Errorf("DeleteTaskResources: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) ListTaskResources(ctx context.Context, ref model.Ref) ([]*model.TaskResourcesEntry, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT task, cpu_request, cpu_limit, memory_request, memory_limit, modified_at
		FROM task_resources
		WHERE project_name = $1 AND dag_name = $2 ORDER BY task`, ref.Project, ref.Name)
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
func (r *Repo) GetTaskResources(ctx context.Context, ref model.Ref, task string) (*model.TaskResources, error) {
	var res model.TaskResources
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT cpu_request, cpu_limit, memory_request, memory_limit
		FROM task_resources
		WHERE project_name = $1 AND dag_name = $2 AND task = $3`, ref.Project, ref.Name, task).
		Scan(&res.CPURequest, &res.CPULimit, &res.MemoryRequest, &res.MemoryLimit)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("GetTaskResources: %w", err)
	}
	return &res, nil
}
