package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mechta-market/mobone/v2"
	moboneTools "github.com/mechta-market/mobone/v2/tools"
	"github.com/samber/lo"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	dagManifest "github.com/rendau/loom/server/internal/domain/dag/manifest"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/project/model"
	repoModel "github.com/rendau/loom/server/internal/domain/project/repo/db/model"
)

var allowedSortFields = map[string]string{
	"name":        "name",
	"created_at":  "created_at",
	"modified_at": "modified_at",
}

type Repo struct {
	*commonRepoPg.Base
	ModelStore *mobone.ModelStore
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{
		Base: base,
		ModelStore: &mobone.ModelStore{
			Con:                base.Con,
			TransactionManager: base.TxM,
			QB:                 base.QB,
			TableName:          "project",
		},
	}
}

func (r *Repo) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	conditions := map[string]any{}
	if pars != nil && pars.AutoUpdate != nil {
		conditions["auto_update"] = *pars.AutoUpdate
	}

	items := make([]*repoModel.Select, 0)

	totalCount, err := r.ModelStore.List(ctx, mobone.ListParams{
		Conditions:     conditions,
		Page:           pars.Page,
		PageSize:       pars.PageSize,
		WithTotalCount: pars.WithTotalCount,
		OnlyCount:      pars.OnlyCount,
		Sort:           moboneTools.ConstructSortColumns(allowedSortFields, pars.Sort),
	}, func(add bool) mobone.ListModelI {
		item := &repoModel.Select{}
		if add {
			items = append(items, item)
		}
		return item
	})
	if err != nil {
		return nil, 0, fmt.Errorf("ModelStore.List: %w", err)
	}
	return lo.Map(items, repoModel.EncodeSelect), totalCount, nil
}

func (r *Repo) Get(ctx context.Context, name string) (*model.Main, bool, error) {
	m := &repoModel.Select{Name: name}
	found, err := r.ModelStore.Get(ctx, m)
	if err != nil {
		return nil, false, fmt.Errorf("ModelStore.Get: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	return repoModel.EncodeSelect(m, 0), true, nil
}

func (r *Repo) UpdateOrCreate(ctx context.Context, name string, obj *model.Edit) error {
	m := repoModel.DecodeUpsert(obj)
	m.PKName = name
	if err := r.ModelStore.UpdateOrCreate(ctx, m); err != nil {
		return fmt.Errorf("ModelStore.UpdateOrCreate: %w", err)
	}
	return nil
}

func (r *Repo) Update(ctx context.Context, name string, obj *model.Edit) error {
	m := repoModel.DecodeUpsert(obj)
	m.PKName = name
	if err := r.ModelStore.Update(ctx, m); err != nil {
		return fmt.Errorf("ModelStore.Update: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, name string) error {
	m := &repoModel.Upsert{PKName: name}
	if err := r.ModelStore.Delete(ctx, m); err != nil {
		return fmt.Errorf("ModelStore.Delete: %w", err)
	}
	return nil
}

// ── шаблоны образа ──────────────────────────────────────────────────────

// SetTemplates переписывает каталог проекта: манифесты валидных дагов
// обновляются, а шаблоны, которых в каталоге не оказалось, помечаются
// orphaned — удалять их нельзя, иначе заведённые от них даги потеряли бы
// последний известный граф. Даг, вернувшийся в образ, снова становится
// живым.
func (r *Repo) SetTemplates(ctx context.Context, project string, items []model.TemplateEdit) error {
	con := r.TxM.GetConnection(ctx)

	for _, item := range items {
		if item.Manifest == nil {
			continue
		}
		_, err := con.Exec(ctx, `
			INSERT INTO dag_template (project_name, name, sdk_version, manifest, orphaned)
			VALUES ($1, $2, $3, $4, false)
			ON CONFLICT (project_name, name) DO UPDATE SET
				sdk_version = excluded.sdk_version,
				manifest = excluded.manifest,
				orphaned = false,
				modified_at = now()`,
			project, item.Name, item.SdkVersion, item.Manifest)
		if err != nil {
			return fmt.Errorf("SetTemplates upsert %q: %w", item.Name, err)
		}
	}

	// шаблон с ошибкой валидации в каталоге сохраняет прежний манифест, но
	// живым уже не считается — как и вовсе пропавший из образа
	alive := lo.FilterMap(items, func(v model.TemplateEdit, _ int) (string, bool) {
		return v.Name, v.Manifest != nil
	})

	_, err := con.Exec(ctx, `
		UPDATE dag_template SET orphaned = true, modified_at = now()
		WHERE project_name = $1 AND NOT (name = ANY($2)) AND NOT orphaned`,
		project, alive)
	if err != nil {
		return fmt.Errorf("SetTemplates orphan: %w", err)
	}
	return nil
}

func (r *Repo) ListTemplates(ctx context.Context, project string) ([]*model.Template, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT t.name, t.sdk_version, t.manifest, t.orphaned, t.created_at, t.modified_at,
		       (SELECT count(*) FROM dag d
		         WHERE d.project_name = t.project_name AND d.template = t.name)
		FROM dag_template t WHERE t.project_name = $1 ORDER BY t.name`, project)
	if err != nil {
		return nil, fmt.Errorf("ListTemplates: %w", err)
	}
	defer rows.Close()

	var result []*model.Template
	for rows.Next() {
		t, err := scanTemplate(project, rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("ListTemplates rows: %w", err)
	}
	return result, nil
}

// GetTemplate — шаблон проекта; found=false — такого дага в образе нет.
func (r *Repo) GetTemplate(ctx context.Context, project, name string) (*model.Template, bool, error) {
	row := r.TxM.GetConnection(ctx).QueryRow(ctx, `
		SELECT t.name, t.sdk_version, t.manifest, t.orphaned, t.created_at, t.modified_at,
		       (SELECT count(*) FROM dag d
		         WHERE d.project_name = t.project_name AND d.template = t.name)
		FROM dag_template t WHERE t.project_name = $1 AND t.name = $2`, project, name)

	t, err := scanTemplate(project, row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return t, true, nil
}

// CountDags — число заведённых дагов по проектам (список проектов).
func (r *Repo) CountDags(ctx context.Context, projects []string) (map[string]int, error) {
	rows, err := r.TxM.GetConnection(ctx).Query(ctx, `
		SELECT project_name, count(*) FROM dag
		WHERE project_name = ANY($1) GROUP BY project_name`, projects)
	if err != nil {
		return nil, fmt.Errorf("CountDags: %w", err)
	}
	defer rows.Close()

	result := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err = rows.Scan(&name, &count); err != nil {
			return nil, fmt.Errorf("CountDags scan: %w", err)
		}
		result[name] = count
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("CountDags rows: %w", err)
	}
	return result, nil
}

// rowScanner — общий интерфейс pgx.Row и pgx.Rows в части Scan.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTemplate(project string, row rowScanner) (*model.Template, error) {
	t := &model.Template{Project: project}
	var modifiedAt *time.Time

	if err := row.Scan(&t.Name, &t.SdkVersion, &t.Manifest, &t.Orphaned,
		&t.CreatedAt, &modifiedAt, &t.DagCount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, fmt.Errorf("scan template: %w", err)
	}
	if modifiedAt != nil {
		t.ModifiedAt = *modifiedAt
	}

	// манифест валидировался при регистрации: ошибка разбора здесь —
	// деградация без паники (пустой граф)
	if m, err := dagManifest.Parse(t.Manifest); err == nil {
		t.MaxActiveRuns = m.MaxActiveRuns
		t.Tasks = m.Tasks
	} else {
		t.Tasks = []dagModel.Task{}
	}

	return t, nil
}
