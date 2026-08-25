package db

import (
	"context"
	"fmt"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/setting/model"
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

func (r *Repo) Set(ctx context.Context, dagName, name, value string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO setting (dag_name, name, value) VALUES ($1, $2, $3)
		ON CONFLICT (dag_name, name) DO UPDATE SET value = excluded.value, modified_at = now()`,
		dagName, name, value)
	if err != nil {
		return fmt.Errorf("Set: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, dagName, name string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx,
		`DELETE FROM setting WHERE dag_name = $1 AND name = $2`, dagName, name)
	if err != nil {
		return false, fmt.Errorf("Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// List — сохранённые значения; dagName nil — все скоупы.
func (r *Repo) List(ctx context.Context, dagName *string) ([]*model.Main, error) {
	query := `SELECT dag_name, name, value, modified_at FROM setting`
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
		if err = rows.Scan(&m.DagName, &m.Name, &m.Value, &m.ModifiedAt); err != nil {
			return nil, fmt.Errorf("List scan: %w", err)
		}
		result = append(result, &m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("List rows: %w", err)
	}
	return result, nil
}

// GetValues — значения по скоупам для резолва: глобальный (пустой
// dag_name) всегда включён, плюс перечисленные даги;
// map[dagName]map[name]value.
func (r *Repo) GetValues(ctx context.Context, dagNames []string) (map[string]map[string]string, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT dag_name, name, value FROM setting
		WHERE dag_name = '' OR dag_name = ANY($1)`, dagNames)
	if err != nil {
		return nil, fmt.Errorf("GetValues: %w", err)
	}
	defer rows.Close()

	result := map[string]map[string]string{}
	for rows.Next() {
		var dagName, name, value string
		if err = rows.Scan(&dagName, &name, &value); err != nil {
			return nil, fmt.Errorf("GetValues scan: %w", err)
		}
		if result[dagName] == nil {
			result[dagName] = map[string]string{}
		}
		result[dagName][name] = value
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetValues rows: %w", err)
	}
	return result, nil
}
