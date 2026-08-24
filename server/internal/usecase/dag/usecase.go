package dag

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"

	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	"github.com/rendau/loom/server/internal/domain/dag/model"
	dagregModel "github.com/rendau/loom/server/internal/domain/dagreg/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

// maxManifestSize — предохранитель на манифест из PushDagManifest.
const maxManifestSize = 1 << 20

type Usecase struct {
	svc           ServiceI
	inspector     ImageInspectorI
	pools         PoolCheckerI
	sink          ManifestSinkI // nil — регистрация не через describe-Job (EXECUTOR != k8s)
	registrations RegistrationsI
	authz         AuthzI
}

func New(svc ServiceI, inspector ImageInspectorI, pools PoolCheckerI, sink ManifestSinkI,
	registrations RegistrationsI, authz AuthzI,
) *Usecase {
	return &Usecase{
		svc: svc, inspector: inspector, pools: pools, sink: sink,
		registrations: registrations, authz: authz,
	}
}

func (u *Usecase) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	if err := util.RequirePageSize(pars.ListParams, 0); err != nil {
		return nil, 0, err
	}
	items, tCount, err := u.svc.List(ctx, pars)
	if err != nil {
		return nil, 0, fmt.Errorf("svc.List: %w", err)
	}
	return items, tCount, nil
}

func (u *Usecase) Get(ctx context.Context, name string) (*model.Main, error) {
	result, _, err := u.svc.Get(ctx, name, true)
	if err != nil {
		return nil, fmt.Errorf("svc.Get: %w", err)
	}
	return result, nil
}

// Register ставит регистрацию дага в очередь и сразу возвращает запись
// регистрации: pull + describe выполняются асинхронно (статус — через
// Get/ListRegistrations). Желаемые настройки (schedule/catchup/paused)
// применяются только если даг создаётся впервые; autoUpdate nil — сохранить
// текущее значение флага.
func (u *Usecase) Register(ctx context.Context, spec dagregModel.EnqueueSpec) (*dagregModel.Main, error) {
	if spec.Image == "" {
		return nil, errs.ImageRequired
	}

	// расписание валидируем до постановки в очередь — ошибка формы должна
	// вернуться сразу, а не статусом failed через минуты pull'а
	if spec.Schedule != nil && *spec.Schedule != "" {
		if _, err := util.CronNext(*spec.Schedule, time.Now()); err != nil {
			return nil, errs.ErrFull{Err: errs.InvalidRequest,
				Desc: fmt.Sprintf("некорректное расписание %q: %v", *spec.Schedule, err)}
		}
	}

	spec.Source = dagregModel.SourceManual
	spec.DagName = ""

	reg, err := u.registrations.Enqueue(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("registrations.Enqueue: %w", err)
	}
	return reg, nil
}

// Process — собственно обработка регистрации из очереди (dagreg.ProcessorI):
// инспекция образа (пиннинг digest + `describe`) → валидация манифеста →
// сохранение. Имя дага берётся из манифеста; повторная регистрация обновляет
// образ и манифест, настройки дага не трогает.
func (u *Usecase) Process(ctx context.Context, reg *dagregModel.Main) (string, error) {
	digest, raw, err := u.inspector.Inspect(ctx, reg.Image)
	if err != nil {
		return reg.DagName, fmt.Errorf("инспекция образа: %w", err)
	}

	m, err := manifest.Parse(raw)
	if err != nil {
		return reg.DagName, fmt.Errorf("разбор манифеста: %w", err)
	}

	// пулы манифеста должны существовать: таск с неизвестным пулом навсегда
	// завис бы в очереди
	pools := lo.Uniq(lo.FilterMap(m.Tasks, func(t model.Task, _ int) (string, bool) {
		return t.Pool, t.Pool != ""
	}))
	if err = u.pools.CheckExist(ctx, pools); err != nil {
		return m.Name, err
	}

	_, existed, err := u.svc.Get(ctx, m.Name, false)
	if err != nil {
		return m.Name, fmt.Errorf("svc.Get: %w", err)
	}

	if _, err = u.svc.Register(ctx, reg.Image, digest, raw, m, reg.AutoUpdate); err != nil {
		return m.Name, fmt.Errorf("svc.Register: %w", err)
	}

	// желаемые настройки — только новому дагу; порядок paused → schedule,
	// чтобы cron не успел стрельнуть до постановки на паузу
	if !existed {
		if reg.Paused != nil && *reg.Paused {
			if err = u.svc.SetPaused(ctx, m.Name, true); err != nil {
				return m.Name, fmt.Errorf("svc.SetPaused: %w", err)
			}
		}
		if reg.Schedule != nil && *reg.Schedule != "" {
			if err = u.svc.SetSchedule(ctx, m.Name, *reg.Schedule, lo.FromPtr(reg.Catchup)); err != nil {
				return m.Name, fmt.Errorf("svc.SetSchedule: %w", err)
			}
		}
	}
	return m.Name, nil
}

// Sync — принудительное обновление дага из registry: перерегистрация его
// текущего образа прямо сейчас, не дожидаясь тика авто-обновления. Образ
// берётся из записи дага (тег, каким его задали при регистрации), поэтому
// pull + describe подтягивают актуальное содержимое тега; digest и манифест
// дага переписываются результатом, настройки — нет.
func (u *Usecase) Sync(ctx context.Context, name string) (*dagregModel.Main, error) {
	if name == "" {
		return nil, errs.IdRequired
	}

	dag, _, err := u.svc.Get(ctx, name, true)
	if err != nil {
		return nil, fmt.Errorf("svc.Get: %w", err)
	}

	reg, err := u.registrations.Enqueue(ctx, dagregModel.EnqueueSpec{
		Image:   dag.Image,
		Source:  dagregModel.SourceManual,
		DagName: dag.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("registrations.Enqueue: %w", err)
	}
	return reg, nil
}

func (u *Usecase) GetRegistration(ctx context.Context, id string) (*dagregModel.Main, error) {
	if id == "" {
		return nil, errs.IdRequired
	}
	result, err := u.registrations.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("registrations.Get: %w", err)
	}
	return result, nil
}

func (u *Usecase) ListRegistrations(ctx context.Context, req *dagregModel.ListReq) ([]*dagregModel.Main, error) {
	items, err := u.registrations.List(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("registrations.List: %w", err)
	}
	return items, nil
}

// PushManifest — приём манифеста от describe-Job'а: доставка
// ожидающей регистрации по одноразовому describe_id.
func (u *Usecase) PushManifest(_ context.Context, id string, manifest []byte, errMsg string) error {
	if id == "" {
		return errs.IdRequired
	}
	if len(manifest) > maxManifestSize {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("manifest too large: %d bytes", len(manifest))}
	}

	if u.sink == nil || !u.sink.Deliver(id, manifest, errMsg) {
		return errs.ErrFull{Err: errs.ObjectNotFound, Desc: "unknown describe id"}
	}
	return nil
}

func (u *Usecase) SetSchedule(ctx context.Context, name, schedule string, catchup bool) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, name); err != nil {
		return err
	}
	if err := u.svc.SetSchedule(ctx, name, schedule, catchup); err != nil {
		return fmt.Errorf("svc.SetSchedule: %w", err)
	}
	return nil
}

func (u *Usecase) SetPaused(ctx context.Context, name string, paused bool) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, name); err != nil {
		return err
	}
	if err := u.svc.SetPaused(ctx, name, paused); err != nil {
		return fmt.Errorf("svc.SetPaused: %w", err)
	}
	return nil
}

func (u *Usecase) SetAutoUpdate(ctx context.Context, name string, autoUpdate bool) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.svc.SetAutoUpdate(ctx, name, autoUpdate); err != nil {
		return fmt.Errorf("svc.SetAutoUpdate: %w", err)
	}
	return nil
}

func (u *Usecase) Delete(ctx context.Context, name string) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.svc.Delete(ctx, name); err != nil {
		return fmt.Errorf("svc.Delete: %w", err)
	}
	return nil
}
