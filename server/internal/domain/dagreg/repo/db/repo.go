package db

import (
	"context"
	"fmt"
	"time"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/dagreg/model"
)

// defaultListLimit — потолок выборки List без явного лимита.
const defaultListLimit = 50

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

func (r *Repo) Create(ctx context.Context, m *model.Main) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO dag_registration
			(id, image, source, schedule, catchup, paused, auto_update, pool, dag_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		m.Id, m.Image, m.Source, m.Schedule, m.Catchup, m.Paused, m.AutoUpdate, m.Pool, m.DagName)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

// ClaimPending забирает до limit ожидающих регистраций в обработку
// (FOR UPDATE SKIP LOCKED — инстансы control plane не конфликтуют).
func (r *Repo) ClaimPending(ctx context.Context, limit int64) ([]*model.Main, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		UPDATE dag_registration SET status = $1, started_at = now()
		WHERE id IN (
			SELECT id FROM dag_registration WHERE status = $2
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $3
		)
		RETURNING `+selectColumns,
		model.StatusRunning, model.StatusPending, limit)
	if err != nil {
		return nil, fmt.Errorf("ClaimPending: %w", err)
	}
	defer rows.Close()
	return scanMains(rows)
}

// Finish завершает регистрацию; dagName дописывается, если стал известен.
func (r *Repo) Finish(ctx context.Context, id, status, errMsg, dagName string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE dag_registration
		SET status = $2, error = $3, dag_name = CASE WHEN $4 <> '' THEN $4 ELSE dag_name END,
			finished_at = now()
		WHERE id = $1`,
		id, status, errMsg, dagName)
	if err != nil {
		return fmt.Errorf("Finish: %w", err)
	}
	return nil
}

func (r *Repo) Get(ctx context.Context, id string) (*model.Main, bool, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx,
		`SELECT `+selectColumns+` FROM dag_registration WHERE id = $1`, id)
	if err != nil {
		return nil, false, fmt.Errorf("Get: %w", err)
	}
	defer rows.Close()

	items, err := scanMains(rows)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		return nil, false, nil
	}
	return items[0], true, nil
}

func (r *Repo) List(ctx context.Context, req *model.ListReq) ([]*model.Main, error) {
	limit := req.Limit
	if limit <= 0 || limit > defaultListLimit {
		limit = defaultListLimit
	}

	query := `SELECT ` + selectColumns + ` FROM dag_registration WHERE true`
	args := []any{}
	if req.DagName != nil {
		args = append(args, *req.DagName)
		query += fmt.Sprintf(" AND dag_name = $%d", len(args))
	}
	if req.OnlyActive {
		args = append(args, []string{model.StatusPending, model.StatusRunning})
		query += fmt.Sprintf(" AND status = ANY($%d)", len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))

	rows, err := r.TxM.GetConnection(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer rows.Close()
	return scanMains(rows)
}

// HasActive — есть ли незавершённая регистрация этого образа (дедуп
// auto-перерегистраций dagsync).
func (r *Repo) HasActive(ctx context.Context, image string) (bool, error) {
	var exists bool
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM dag_registration
			WHERE image = $1 AND status = ANY($2)
		)`, image, []string{model.StatusPending, model.StatusRunning}).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("HasActive: %w", err)
	}
	return exists, nil
}

// FailStale — running-записи, начатые раньше порога, в failed: инстанс,
// который их claim'нул, умер посреди describe.
func (r *Repo) FailStale(ctx context.Context, startedBefore time.Time) (int64, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE dag_registration
		SET status = $1, error = 'обработка прервана (рестарт control plane)', finished_at = now()
		WHERE status = $2 AND started_at < $3`,
		model.StatusFailed, model.StatusRunning, startedBefore)
	if err != nil {
		return 0, fmt.Errorf("FailStale: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteFinishedBefore — чистка завершённых записей старше порога.
func (r *Repo) DeleteFinishedBefore(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		DELETE FROM dag_registration
		WHERE status = ANY($1) AND finished_at < $2`,
		[]string{model.StatusSuccess, model.StatusFailed}, before)
	if err != nil {
		return 0, fmt.Errorf("DeleteFinishedBefore: %w", err)
	}
	return tag.RowsAffected(), nil
}

const selectColumns = `id, image, source, schedule, catchup, paused, auto_update, pool,
	status, error, dag_name, created_at, started_at, finished_at`

type pgRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanMains(rows pgRows) ([]*model.Main, error) {
	var result []*model.Main
	for rows.Next() {
		var m model.Main
		var startedAt, finishedAt *time.Time
		err := rows.Scan(&m.Id, &m.Image, &m.Source, &m.Schedule, &m.Catchup, &m.Paused,
			&m.AutoUpdate, &m.Pool, &m.Status, &m.Error, &m.DagName, &m.CreatedAt, &startedAt, &finishedAt)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if startedAt != nil {
			m.StartedAt = *startedAt
		}
		if finishedAt != nil {
			m.FinishedAt = *finishedAt
		}
		result = append(result, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return result, nil
}
