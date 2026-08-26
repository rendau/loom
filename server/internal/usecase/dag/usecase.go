package dag

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"

	"github.com/rendau/loom/server/internal/domain/dag/model"
	statsModel "github.com/rendau/loom/server/internal/domain/stats/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

type Usecase struct {
	svc      ServiceI
	projects ProjectsI
	pools    PoolCheckerI
	stats    StatsI
	authz    AuthzI
}

func New(svc ServiceI, projects ProjectsI, pools PoolCheckerI, stats StatsI, authz AuthzI) *Usecase {
	return &Usecase{svc: svc, projects: projects, pools: pools, stats: stats, authz: authz}
}

const lastRunsPerDag = 5

func (u *Usecase) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	if err := util.RequirePageSize(pars.ListParams, 0); err != nil {
		return nil, 0, err
	}
	items, tCount, err := u.svc.List(ctx, pars)
	if err != nil {
		return nil, 0, fmt.Errorf("svc.List: %w", err)
	}

	// статус-стрип: последние раны дособираются одним запросом на страницу
	lastRuns, err := u.svc.ListLastRuns(ctx,
		lo.Map(items, func(d *model.Main, _ int) model.Ref { return d.Ref }), lastRunsPerDag)
	if err != nil {
		return nil, 0, fmt.Errorf("svc.ListLastRuns: %w", err)
	}
	for _, d := range items {
		d.LastRuns = lastRuns[d.Ref]
	}

	return items, tCount, nil
}

func (u *Usecase) Get(ctx context.Context, ref model.Ref) (*model.Main, error) {
	if ref.Empty() {
		return nil, errs.IdRequired
	}

	result, _, err := u.svc.Get(ctx, ref, true)
	if err != nil {
		return nil, fmt.Errorf("svc.Get: %w", err)
	}

	lastRuns, err := u.svc.ListLastRuns(ctx, []model.Ref{ref}, lastRunsPerDag)
	if err != nil {
		return nil, fmt.Errorf("svc.ListLastRuns: %w", err)
	}
	result.LastRuns = lastRuns[ref]

	return result, nil
}

// CreateSpec — заведение дага-инстанса от шаблона образа с желаемыми
// настройками (nil — не задано).
type CreateSpec struct {
	Project  string
	Template string
	Name     string // пусто — имя шаблона
	Schedule *string
	Catchup  *bool
	Paused   *bool
	Pool     *string
}

// Create заводит даг от шаблона проекта: от одного шаблона инстансов может
// быть сколько угодно, различаются они настройками и своими переменными.
func (u *Usecase) Create(ctx context.Context, spec CreateSpec) (*model.Main, error) {
	if spec.Project == "" || spec.Template == "" {
		return nil, errs.IdRequired
	}
	if err := u.authz.RequireProject(ctx, spec.Project); err != nil {
		return nil, err
	}

	// шаблон должен быть в образе: инстанс без манифеста нечем запускать
	if _, _, err := u.projects.GetTemplate(ctx, spec.Project, spec.Template, true); err != nil {
		return nil, fmt.Errorf("projects.GetTemplate: %w", err)
	}

	// расписание и пул валидируем до создания — ошибку формы нужно вернуть
	// сразу, а не оставлять полусозданный даг
	if spec.Schedule != nil && *spec.Schedule != "" {
		if _, err := util.CronNext(*spec.Schedule, time.Now()); err != nil {
			return nil, errs.ErrFull{Err: errs.InvalidRequest,
				Desc: fmt.Sprintf("некорректное расписание %q: %v", *spec.Schedule, err)}
		}
	}
	if spec.Pool != nil && *spec.Pool != "" {
		if err := u.pools.CheckExist(ctx, []string{*spec.Pool}); err != nil {
			return nil, err
		}
	}

	name := spec.Name
	if name == "" {
		name = spec.Template
	}
	ref := model.NewRef(spec.Project, name)

	result, err := u.svc.Create(ctx, ref, spec.Template)
	if err != nil {
		return nil, fmt.Errorf("svc.Create: %w", err)
	}

	// порядок paused → schedule: cron не должен стрельнуть до паузы
	if spec.Paused != nil && *spec.Paused {
		if err = u.svc.SetPaused(ctx, ref, true); err != nil {
			return nil, fmt.Errorf("svc.SetPaused: %w", err)
		}
	}
	if spec.Pool != nil && *spec.Pool != "" {
		if err = u.svc.SetPool(ctx, ref, *spec.Pool); err != nil {
			return nil, fmt.Errorf("svc.SetPool: %w", err)
		}
	}
	if spec.Schedule != nil && *spec.Schedule != "" {
		if err = u.svc.SetSchedule(ctx, ref, *spec.Schedule, lo.FromPtr(spec.Catchup)); err != nil {
			return nil, fmt.Errorf("svc.SetSchedule: %w", err)
		}
	}

	if spec.Paused != nil || spec.Pool != nil || spec.Schedule != nil {
		if result, _, err = u.svc.Get(ctx, ref, true); err != nil {
			return nil, fmt.Errorf("svc.Get: %w", err)
		}
	}
	return result, nil
}

// GetStats — агрегаты по таскам дага за последние lastRuns завершённых
// ранов («жирные таски»); lastRuns 0 — дефолт, потолок — защита от тяжёлых
// запросов.
func (u *Usecase) GetStats(ctx context.Context, ref model.Ref, lastRuns int64) (int64, []statsModel.TaskStat, error) {
	if ref.Empty() {
		return 0, nil, errs.IdRequired
	}
	if _, _, err := u.svc.Get(ctx, ref, true); err != nil {
		return 0, nil, fmt.Errorf("svc.Get: %w", err)
	}

	switch {
	case lastRuns <= 0:
		lastRuns = 20
	case lastRuns > 100:
		lastRuns = 100
	}

	runs, stats, err := u.stats.DagStats(ctx, ref, lastRuns)
	if err != nil {
		return 0, nil, fmt.Errorf("stats.DagStats: %w", err)
	}
	return runs, stats, nil
}

// ListTaskResources — оверрайды ресурсов тасков дага (читают все
// аутентифицированные, как и сам даг).
func (u *Usecase) ListTaskResources(ctx context.Context, ref model.Ref) ([]*model.TaskResourcesEntry, error) {
	if ref.Empty() {
		return nil, errs.IdRequired
	}
	items, err := u.svc.ListTaskResources(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("svc.ListTaskResources: %w", err)
	}
	return items, nil
}

func (u *Usecase) SetTaskResources(ctx context.Context, ref model.Ref, task string, res model.TaskResources) error {
	if ref.Empty() || task == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, ref); err != nil {
		return err
	}
	if err := u.svc.SetTaskResources(ctx, ref, task, res); err != nil {
		return fmt.Errorf("svc.SetTaskResources: %w", err)
	}
	return nil
}

func (u *Usecase) DeleteTaskResources(ctx context.Context, ref model.Ref, task string) error {
	if ref.Empty() || task == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, ref); err != nil {
		return err
	}
	if err := u.svc.DeleteTaskResources(ctx, ref, task); err != nil {
		return fmt.Errorf("svc.DeleteTaskResources: %w", err)
	}
	return nil
}

func (u *Usecase) SetSchedule(ctx context.Context, ref model.Ref, schedule string, catchup bool) error {
	if ref.Empty() {
		return errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, ref); err != nil {
		return err
	}
	if err := u.svc.SetSchedule(ctx, ref, schedule, catchup); err != nil {
		return fmt.Errorf("svc.SetSchedule: %w", err)
	}
	return nil
}

func (u *Usecase) SetPaused(ctx context.Context, ref model.Ref, paused bool) error {
	if ref.Empty() {
		return errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, ref); err != nil {
		return err
	}
	if err := u.svc.SetPaused(ctx, ref, paused); err != nil {
		return fmt.Errorf("svc.SetPaused: %w", err)
	}
	return nil
}

// SetPool задаёт (или снимает — пустая строка) пул слотов дага. Пул
// действует на все таски дага; смена применяется со следующего рана.
func (u *Usecase) SetPool(ctx context.Context, ref model.Ref, pool string) error {
	if ref.Empty() {
		return errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, ref); err != nil {
		return err
	}
	// неизвестный пул навсегда оставил бы таски дага в очереди
	if pool != "" {
		if err := u.pools.CheckExist(ctx, []string{pool}); err != nil {
			return err
		}
	}
	if err := u.svc.SetPool(ctx, ref, pool); err != nil {
		return fmt.Errorf("svc.SetPool: %w", err)
	}
	return nil
}

func (u *Usecase) Delete(ctx context.Context, ref model.Ref) error {
	if ref.Empty() {
		return errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, ref); err != nil {
		return err
	}
	if err := u.svc.Delete(ctx, ref); err != nil {
		return fmt.Errorf("svc.Delete: %w", err)
	}
	return nil
}
