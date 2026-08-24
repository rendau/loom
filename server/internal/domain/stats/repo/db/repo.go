// Package db — SQL-агрегаты дашборда. Отдельный repo (а не выборка ранов
// пачками в память): счётчики за неделю считает Postgres, а не админка.
package db

import (
	"context"
	"fmt"
	"time"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/stats/model"
)

// Лимиты списков дашборда.
const (
	upcomingLimit = 10
	failuresLimit = 10
	durationLimit = 10
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

// Counters — активные раны и общее число дагов.
func (r *Repo) Counters(ctx context.Context) (activeRuns, dags, pausedDags int64, err error) {
	err = r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM run WHERE status = 'running'),
			(SELECT count(*) FROM dag),
			(SELECT count(*) FROM dag WHERE paused)`).Scan(&activeRuns, &dags, &pausedDags)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("Counters: %w", err)
	}
	return activeRuns, dags, pausedDags, nil
}

// Window — исходы ранов, завершённых после since.
func (r *Repo) Window(ctx context.Context, since time.Time) (model.Window, error) {
	var w model.Window
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'success'),
			count(*) FILTER (WHERE status = 'failed')
		FROM run WHERE finished_at >= $1`, since).Scan(&w.Success, &w.Failed)
	if err != nil {
		return model.Window{}, fmt.Errorf("Window: %w", err)
	}
	return w, nil
}

func (r *Repo) Upcoming(ctx context.Context) ([]model.Upcoming, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT name, next_run_at, schedule FROM dag
		WHERE NOT paused AND schedule <> '' AND next_run_at IS NOT NULL
		ORDER BY next_run_at LIMIT $1`, upcomingLimit)
	if err != nil {
		return nil, fmt.Errorf("Upcoming: %w", err)
	}
	defer rows.Close()

	var result []model.Upcoming
	for rows.Next() {
		var u model.Upcoming
		if err = rows.Scan(&u.DagName, &u.NextRunAt, &u.Schedule); err != nil {
			return nil, fmt.Errorf("Upcoming scan: %w", err)
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

// Pools — слоты и занятость (попытки в starting/running).
func (r *Repo) Pools(ctx context.Context) ([]model.PoolUsage, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT p.name, p.slots,
			(SELECT count(*) FROM task_instance ti
			 WHERE ti.pool = p.name AND ti.status IN ('starting', 'running'))
		FROM pool p ORDER BY p.name`)
	if err != nil {
		return nil, fmt.Errorf("Pools: %w", err)
	}
	defer rows.Close()

	var result []model.PoolUsage
	for rows.Next() {
		var p model.PoolUsage
		if err = rows.Scan(&p.Name, &p.Slots, &p.Busy); err != nil {
			return nil, fmt.Errorf("Pools scan: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *Repo) RecentFailures(ctx context.Context) ([]model.Failure, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT id, dag_name, finished_at FROM run
		WHERE status = 'failed' AND finished_at IS NOT NULL
		ORDER BY finished_at DESC LIMIT $1`, failuresLimit)
	if err != nil {
		return nil, fmt.Errorf("RecentFailures: %w", err)
	}
	defer rows.Close()

	var result []model.Failure
	for rows.Next() {
		var f model.Failure
		if err = rows.Scan(&f.RunId, &f.DagName, &f.FinishedAt); err != nil {
			return nil, fmt.Errorf("RecentFailures scan: %w", err)
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

// Activity — раны по дням за последние days суток (UTC), включая пустые дни.
func (r *Repo) Activity(ctx context.Context, days int) ([]model.Day, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT to_char(d.day, 'YYYY-MM-DD'),
			count(r.id) FILTER (WHERE r.status = 'success'),
			count(r.id) FILTER (WHERE r.status = 'failed'),
			count(r.id) FILTER (WHERE r.status = 'running')
		FROM generate_series(
			date_trunc('day', now() at time zone 'UTC') - make_interval(days => $1 - 1),
			date_trunc('day', now() at time zone 'UTC'),
			interval '1 day'
		) AS d(day)
		LEFT JOIN run r ON date_trunc('day', r.created_at at time zone 'UTC') = d.day
		GROUP BY d.day ORDER BY d.day`, days)
	if err != nil {
		return nil, fmt.Errorf("Activity: %w", err)
	}
	defer rows.Close()

	var result []model.Day
	for rows.Next() {
		var d model.Day
		if err = rows.Scan(&d.Date, &d.Success, &d.Failed, &d.Running); err != nil {
			return nil, fmt.Errorf("Activity scan: %w", err)
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

// DagDurations — среднее и максимальное время завершённых ранов дага после
// since (самые долгие первыми).
func (r *Repo) DagDurations(ctx context.Context, since time.Time) ([]model.DagDuration, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT dag_name,
			avg(extract(epoch FROM finished_at - created_at)),
			max(extract(epoch FROM finished_at - created_at)),
			count(*)
		FROM run
		WHERE finished_at IS NOT NULL AND finished_at >= $1
		GROUP BY dag_name
		ORDER BY avg(extract(epoch FROM finished_at - created_at)) DESC
		LIMIT $2`, since, durationLimit)
	if err != nil {
		return nil, fmt.Errorf("DagDurations: %w", err)
	}
	defer rows.Close()

	var result []model.DagDuration
	for rows.Next() {
		var d model.DagDuration
		if err = rows.Scan(&d.DagName, &d.AvgSec, &d.MaxSec, &d.Runs); err != nil {
			return nil, fmt.Errorf("DagDurations scan: %w", err)
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
