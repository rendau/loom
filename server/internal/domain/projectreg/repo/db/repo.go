package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/projectreg/model"
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
		INSERT INTO project_registration
			(id, project_name, image, source, auto_update, create_dags)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		m.Id, m.ProjectName, m.Image, m.Source, m.AutoUpdate, m.CreateDags)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

// ClaimPending забирает до limit ожидающих регистраций в обработку
// (FOR UPDATE SKIP LOCKED — инстансы control plane не конфликтуют).
func (r *Repo) ClaimPending(ctx context.Context, limit int64) ([]*model.Main, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		UPDATE project_registration SET status = $1, started_at = now()
		WHERE id IN (
			SELECT id FROM project_registration WHERE status = $2
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

// Finish завершает регистрацию, дописывая итог по дагам образа.
func (r *Repo) Finish(ctx context.Context, id, status, errMsg string, result []model.DagResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("Finish marshal result: %w", err)
	}
	if result == nil {
		raw = []byte("[]")
	}

	_, err = r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE project_registration
		SET status = $2, error = $3, result = $4, finished_at = now()
		WHERE id = $1`,
		id, status, errMsg, raw)
	if err != nil {
		return fmt.Errorf("Finish: %w", err)
	}
	return nil
}

func (r *Repo) Get(ctx context.Context, id string) (*model.Main, bool, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx,
		`SELECT `+selectColumns+` FROM project_registration WHERE id = $1`, id)
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

	query := `SELECT ` + selectColumns + ` FROM project_registration WHERE true`
	args := []any{}
	if req.ProjectName != nil {
		args = append(args, *req.ProjectName)
		query += fmt.Sprintf(" AND project_name = $%d", len(args))
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

// HasActive — есть ли незавершённая регистрация этого проекта (дедуп
// auto-перерегистраций dagsync).
func (r *Repo) HasActive(ctx context.Context, projectName string) (bool, error) {
	var exists bool
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM project_registration
			WHERE project_name = $1 AND status = ANY($2)
		)`, projectName, []string{model.StatusPending, model.StatusRunning}).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("HasActive: %w", err)
	}
	return exists, nil
}

// FailStale — running-записи, начатые раньше порога, в failed: инстанс,
// который их claim'нул, умер посреди describe.
func (r *Repo) FailStale(ctx context.Context, startedBefore time.Time) (int64, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE project_registration
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
		DELETE FROM project_registration
		WHERE status = ANY($1) AND finished_at < $2`,
		[]string{model.StatusSuccess, model.StatusFailed}, before)
	if err != nil {
		return 0, fmt.Errorf("DeleteFinishedBefore: %w", err)
	}
	return tag.RowsAffected(), nil
}

const selectColumns = `id, project_name, image, source, auto_update, create_dags,
	status, error, result, created_at, started_at, finished_at`

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
		var rawResult []byte

		err := rows.Scan(&m.Id, &m.ProjectName, &m.Image, &m.Source, &m.AutoUpdate,
			&m.CreateDags, &m.Status, &m.Error, &rawResult, &m.CreatedAt, &startedAt, &finishedAt)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if len(rawResult) > 0 {
			if err = json.Unmarshal(rawResult, &m.Result); err != nil {
				return nil, fmt.Errorf("unmarshal result: %w", err)
			}
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
