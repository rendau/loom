package db

import (
	"context"
	"fmt"
	"time"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/variable/model"
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

func (r *Repo) Set(ctx context.Context, dagName, name, value string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO variable (dag_name, name, value) VALUES ($1, $2, $3)
		ON CONFLICT (dag_name, name) DO UPDATE SET value = excluded.value, modified_at = now()`,
		dagName, name, value)
	if err != nil {
		return fmt.Errorf("Set: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, dagName, name string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx,
		`DELETE FROM variable WHERE dag_name = $1 AND name = $2`, dagName, name)
	if err != nil {
		return false, fmt.Errorf("Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// List — переменные со значениями; dagName nil — все скоупы.
func (r *Repo) List(ctx context.Context, dagName *string) ([]*model.Main, error) {
	query := `SELECT dag_name, name, value, created_at, modified_at FROM variable`
	args := []any{}
	if dagName != nil {
		args = append(args, *dagName)
		query += ` WHERE dag_name = $1`
	}
	query += ` ORDER BY dag_name, name`

	rows, err := r.TxM.GetConnection(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer rows.Close()

	var result []*model.Main
	for rows.Next() {
		var m model.Main
		var modifiedAt *time.Time
		if err = rows.Scan(&m.DagName, &m.Name, &m.Value, &m.CreatedAt, &modifiedAt); err != nil {
			return nil, fmt.Errorf("List scan: %w", err)
		}
		if modifiedAt != nil {
			m.ModifiedAt = *modifiedAt
		}
		result = append(result, &m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("List rows: %w", err)
	}
	return result, nil
}

// GetValues возвращает значения переменных (со скоупом-источником) по
// именам для дага: локальный скоуп перекрывает глобальный; отсутствующие
// имена в результат не попадают.
func (r *Repo) GetValues(ctx context.Context, dagName string, names []string) (map[string]model.Resolved, error) {
	// сортировка по dag_name кладёт глобальные ('') первыми — локальные
	// перезапишут их в map
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT name, value, dag_name FROM variable
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
