package db

import (
	"context"
	"fmt"
	"time"

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
