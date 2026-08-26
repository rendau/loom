package db

import (
	"context"
	"fmt"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/setting/model"
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

func (r *Repo) Set(ctx context.Context, scope commonModel.Scope, name, value string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO setting (project_name, dag_name, name, value) VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_name, dag_name, name)
		DO UPDATE SET value = excluded.value, modified_at = now()`,
		scope.Project, scope.Dag, name, value)
	if err != nil {
		return fmt.Errorf("Set: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, scope commonModel.Scope, name string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx,
		`DELETE FROM setting WHERE project_name = $1 AND dag_name = $2 AND name = $3`,
		scope.Project, scope.Dag, name)
	if err != nil {
		return false, fmt.Errorf("Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// List — сохранённые значения; scope nil — все скоупы.
func (r *Repo) List(ctx context.Context, scope *commonModel.Scope) ([]*model.Main, error) {
	query := `SELECT project_name, dag_name, name, value, modified_at FROM setting`
	args := []any{}
	if scope != nil {
		args = append(args, scope.Project, scope.Dag)
		query += ` WHERE project_name = $1 AND dag_name = $2`
	}
	query += ` ORDER BY project_name, dag_name, name`

	rows, err := r.TxM.GetConnection(ctx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer rows.Close()

	var result []*model.Main
	for rows.Next() {
		var m model.Main
		if err = rows.Scan(&m.Scope.Project, &m.Scope.Dag, &m.Name, &m.Value, &m.ModifiedAt); err != nil {
			return nil, fmt.Errorf("List scan: %w", err)
		}
		result = append(result, &m)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("List rows: %w", err)
	}
	return result, nil
}

// GetValues — значения по скоупам для резолва: глобальный всегда включён,
// плюс скоупы перечисленных дагов и их проектов.
func (r *Repo) GetValues(ctx context.Context, scopes []commonModel.Scope) (map[commonModel.Scope]map[string]string, error) {
	projects := make([]string, 0, len(scopes))
	dags := make([]string, 0, len(scopes))
	for _, s := range scopes {
		projects = append(projects, s.Project)
		dags = append(dags, s.Dag)
	}

	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT project_name, dag_name, name, value FROM setting
		WHERE (project_name = '' AND dag_name = '')
		   OR (dag_name = '' AND project_name = ANY($1))
		   OR (project_name, dag_name) IN (SELECT * FROM unnest($1::text[], $2::text[]))`,
		projects, dags)
	if err != nil {
		return nil, fmt.Errorf("GetValues: %w", err)
	}
	defer rows.Close()

	result := map[commonModel.Scope]map[string]string{}
	for rows.Next() {
		var scope commonModel.Scope
		var name, value string
		if err = rows.Scan(&scope.Project, &scope.Dag, &name, &value); err != nil {
			return nil, fmt.Errorf("GetValues scan: %w", err)
		}
		if result[scope] == nil {
			result[scope] = map[string]string{}
		}
		result[scope][name] = value
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetValues rows: %w", err)
	}
	return result, nil
}
