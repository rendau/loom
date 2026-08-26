package db

import (
	"context"
	"fmt"
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/variable/model"
)

type Repo struct {
	*commonRepoPg.Base
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{Base: base}
}

func (r *Repo) Set(ctx context.Context, scope commonModel.Scope, name, value string) error {
	_, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		INSERT INTO variable (project_name, dag_name, name, value) VALUES ($1, $2, $3, $4)
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
		`DELETE FROM variable WHERE project_name = $1 AND dag_name = $2 AND name = $3`,
		scope.Project, scope.Dag, name)
	if err != nil {
		return false, fmt.Errorf("Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Exists — есть ли запись с таким именем в скоупе (проверка перед
// переносом: занятое имя — понятная ошибка вместо конфликта в UPDATE).
func (r *Repo) Exists(ctx context.Context, scope commonModel.Scope, name string) (bool, error) {
	var exists bool
	err := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM variable
			WHERE project_name = $1 AND dag_name = $2 AND name = $3)`,
		scope.Project, scope.Dag, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("Exists: %w", err)
	}
	return exists, nil
}

// Move переносит запись в другой скоуп: значение и created_at остаются
// прежними — переезжает только адрес. false — записи в исходном скоупе
// нет. Занятое имя в целевом скоупе ловится уникальным индексом, поэтому
// проверка-и-перенос не разъезжаются между конкурентными запросами.
func (r *Repo) Move(ctx context.Context, from, to commonModel.Scope, name string) (bool, error) {
	tag, err := r.TxM.GetConnection(ctx).Exec(ctx, `
		UPDATE variable SET project_name = $1, dag_name = $2, modified_at = now()
		WHERE project_name = $3 AND dag_name = $4 AND name = $5`,
		to.Project, to.Dag, from.Project, from.Dag, name)
	if err != nil {
		return false, fmt.Errorf("Move: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// List — переменные со значениями; scope nil — все скоупы.
func (r *Repo) List(ctx context.Context, scope *commonModel.Scope) ([]*model.Main, error) {
	query := `SELECT project_name, dag_name, name, value, created_at, modified_at FROM variable`
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
		var modifiedAt *time.Time
		if err = rows.Scan(&m.Scope.Project, &m.Scope.Dag, &m.Name, &m.Value,
			&m.CreatedAt, &modifiedAt); err != nil {
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
// именам для скоупа дага: даг перекрывает проект, проект — глобальный;
// отсутствующие имена в результат не попадают.
func (r *Repo) GetValues(ctx context.Context, scope commonModel.Scope, names []string) (map[string]model.Resolved, error) {
	// сортировка по (project_name, dag_name) кладёт глобальные ('','')
	// первыми, затем проектные, затем дага — каждый следующий уровень
	// перезаписывает предыдущий в map
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT name, value, project_name, dag_name FROM variable
		WHERE name = ANY($3)
		  AND (project_name, dag_name) IN (('', ''), ($1, ''), ($1, $2))
		ORDER BY project_name, dag_name`, scope.Project, scope.Dag, names)
	if err != nil {
		return nil, fmt.Errorf("GetValues: %w", err)
	}
	defer rows.Close()

	result := map[string]model.Resolved{}
	for rows.Next() {
		var name string
		var resolved model.Resolved
		if err = rows.Scan(&name, &resolved.Value,
			&resolved.Scope.Project, &resolved.Scope.Dag); err != nil {
			return nil, fmt.Errorf("GetValues scan: %w", err)
		}
		result[name] = resolved
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("GetValues rows: %w", err)
	}
	return result, nil
}
