package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/secret/model"
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

func (r *Repo) Set(ctx context.Context, dagName, name string, value []byte) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO secret (dag_name, name, value) VALUES ($1, $2, $3)
		ON CONFLICT (dag_name, name) DO UPDATE SET value = excluded.value, modified_at = now()`,
		dagName, name, value)
	if err != nil {
		return fmt.Errorf("Set: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, dagName, name string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx,
		`DELETE FROM secret WHERE dag_name = $1 AND name = $2`, dagName, name)
	if err != nil {
		return false, fmt.Errorf("Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListMeta — метаданные секретов; dagName nil — все скоупы.
func (r *Repo) ListMeta(ctx context.Context, dagName *string) ([]*model.Meta, error) {
	query := `SELECT dag_name, name, created_at, modified_at FROM secret`
	args := []any{}
	if dagName != nil {
		args = append(args, *dagName)
		query += ` WHERE dag_name = $1`
	}
	query += ` ORDER BY dag_name, name`

	rows, err := r.TxM.GetConnection(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ListMeta: %w", err)
	}
	defer rows.Close()

	var result []*model.Meta
	for rows.Next() {
		var m model.Meta
		var modifiedAt *time.Time
		if err = rows.Scan(&m.DagName, &m.Name, &m.CreatedAt, &modifiedAt); err != nil {
			return nil, fmt.Errorf("ListMeta scan: %w", err)
		}
		if modifiedAt != nil {
			m.ModifiedAt = *modifiedAt
		}
		result = append(result, &m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListMeta rows: %w", err)
	}
	return result, nil
}

// GetValues возвращает сырые (возможно зашифрованные) значения секретов
// (со скоупом-источником) по именам для дага: локальный скоуп перекрывает
// глобальный; отсутствующие имена в результат не попадают.
func (r *Repo) GetValues(ctx context.Context, dagName string, names []string) (map[string]model.Resolved, error) {
	// сортировка по dag_name кладёт глобальные ('') первыми — локальные
	// перезапишут их в map
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT name, value, dag_name FROM secret
		WHERE name = ANY($2) AND dag_name IN ('', $1)
		ORDER BY dag_name`, dagName, names)
	if err != nil {
		return nil, fmt.Errorf("GetValues: %w", err)
	}
	defer rows.Close()

	result := map[string]model.Resolved{}
	for rows.Next() {
		var name string
		var resolved model.Resolved
		if err = rows.Scan(&name, &resolved.Value, &resolved.Scope); err != nil {
			return nil, fmt.Errorf("GetValues scan: %w", err)
		}
		result[name] = resolved
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetValues rows: %w", err)
	}
	return result, nil
}

// GetValue — сырое значение одного секрета точного скоупа (без override).
func (r *Repo) GetValue(ctx context.Context, dagName, name string) ([]byte, bool, error) {
	var value []byte
	err := r.TxM.GetConnection(ctx).QueryRow(ctx,
		`SELECT value FROM secret WHERE dag_name = $1 AND name = $2`, dagName, name).Scan(&value)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("GetValue: %w", err)
	}
	return value, true, nil
}
