// Package project — usecase проектов: регистрация образа (очередь +
// обработка), каталог его дагов и настройки проекта.
package project

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/samber/lo"

	dagManifest "github.com/rendau/loom/server/internal/domain/dag/manifest"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/project/model"
	projectregModel "github.com/rendau/loom/server/internal/domain/projectreg/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

// maxCatalogSize — предохранитель на каталог из PushDagCatalog.
const maxCatalogSize = 4 << 20

type Usecase struct {
	svc           ServiceI
	dags          DagServiceI
	inspector     ImageInspectorI
	sizer         ImageSizerI  // nil — размер образа не показываем
	sink          CatalogSinkI // nil — регистрация не через describe-Job (EXECUTOR != k8s)
	registrations RegistrationsI
	authz         AuthzI
}

func New(svc ServiceI, dags DagServiceI, inspector ImageInspectorI, sizer ImageSizerI,
	sink CatalogSinkI, registrations RegistrationsI, authz AuthzI,
) *Usecase {
	return &Usecase{
		svc: svc, dags: dags, inspector: inspector, sizer: sizer, sink: sink,
		registrations: registrations, authz: authz,
	}
}

// imageSize — размер образа по registry, best effort: он справочный, и
// недоступный (или приватный без кредов) registry не должен валить
// регистрацию. 0 — неизвестен, прежнее значение проекта сохранится.
func (u *Usecase) imageSize(ctx context.Context, image string) int64 {
	if u.sizer == nil {
		return 0
	}
	size, err := u.sizer.ResolveSize(ctx, image)
	if err != nil {
		slog.Info("resolve image size failed", "image", image, "err", err)
		return 0
	}
	return size
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

// Get отдаёт проект вместе с каталогом его образа.
func (u *Usecase) Get(ctx context.Context, name string) (*model.Main, error) {
	if name == "" {
		return nil, errs.IdRequired
	}

	result, _, err := u.svc.Get(ctx, name, true)
	if err != nil {
		return nil, fmt.Errorf("svc.Get: %w", err)
	}

	templates, err := u.svc.ListTemplates(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("svc.ListTemplates: %w", err)
	}
	result.Templates = lo.Map(templates, func(t *model.Template, _ int) model.Template { return *t })
	result.DagCount = lo.SumBy(templates, func(t *model.Template) int { return t.DagCount })

	return result, nil
}

// Register ставит регистрацию проекта в очередь и сразу возвращает запись:
// pull + describe выполняются асинхронно (статус — через
// Get/ListRegistrations). Новый проект заводит только admin; перерегистрация
// существующего доступна и владельцу проекта.
func (u *Usecase) Register(ctx context.Context, spec projectregModel.EnqueueSpec) (*projectregModel.Main, error) {
	if spec.Image == "" {
		return nil, errs.ImageRequired
	}
	if !dagManifest.ValidName(spec.ProjectName) {
		return nil, errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("недопустимое имя проекта %q", spec.ProjectName)}
	}

	_, existed, err := u.svc.Get(ctx, spec.ProjectName, false)
	if err != nil {
		return nil, fmt.Errorf("svc.Get: %w", err)
	}
	if existed {
		if err = u.authz.RequireProject(ctx, spec.ProjectName); err != nil {
			return nil, err
		}
	} else if err = u.authz.RequireAdmin(ctx); err != nil {
		return nil, err
	}

	spec.Source = projectregModel.SourceManual

	reg, err := u.registrations.Enqueue(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("registrations.Enqueue: %w", err)
	}
	return reg, nil
}

// Sync — принудительное обновление проекта из registry: перерегистрация
// его текущего образа прямо сейчас, не дожидаясь тика авто-обновления.
// Образ берётся из записи проекта (тег, каким его задали при регистрации),
// поэтому pull + describe подтягивают актуальное содержимое тега; digest и
// шаблоны переписываются результатом, настройки дагов — нет. Права шире
// прочих операций проекта: sync доступен и владельцу дага проекта — так он
// выкатывает новый код своего дага.
func (u *Usecase) Sync(ctx context.Context, name string) (*projectregModel.Main, error) {
	if name == "" {
		return nil, errs.IdRequired
	}

	p, _, err := u.svc.Get(ctx, name, true)
	if err != nil {
		return nil, fmt.Errorf("svc.Get: %w", err)
	}
	if err = u.authz.RequireProjectSync(ctx, name); err != nil {
		return nil, err
	}

	reg, err := u.registrations.Enqueue(ctx, projectregModel.EnqueueSpec{
		ProjectName: p.Name,
		Image:       p.Image,
		Source:      projectregModel.SourceManual,
		// синк обновляет шаблоны, а заводить даги по новым — решение админа
		CreateDags: false,
	})
	if err != nil {
		return nil, fmt.Errorf("registrations.Enqueue: %w", err)
	}
	return reg, nil
}

// Process — обработка регистрации из очереди (projectreg.ProcessorI):
// инспекция образа (пиннинг digest + `describe`) → разбор каталога →
// сохранение проекта и его шаблонов → заведение дагов по новым шаблонам.
// Возвращает итог по дагам образа — он виден в админке.
func (u *Usecase) Process(ctx context.Context, reg *projectregModel.Main) ([]projectregModel.DagResult, error) {
	digest, raw, err := u.inspector.Inspect(ctx, reg.Image)
	if err != nil {
		return nil, fmt.Errorf("инспекция образа: %w", err)
	}

	catalog, err := dagManifest.ParseCatalog(raw)
	if err != nil {
		return nil, fmt.Errorf("разбор каталога образа: %w", err)
	}

	_, templates, err := u.svc.Register(ctx, model.RegisterSpec{
		Name:           reg.ProjectName,
		Image:          reg.Image,
		ImageDigest:    digest,
		ImageSizeBytes: u.imageSize(ctx, reg.Image),
		AutoUpdate:     reg.AutoUpdate,
	}, catalog)
	result := lo.Map(templates, func(t model.TemplateEdit, _ int) projectregModel.DagResult {
		return projectregModel.DagResult{Name: t.Name, Error: t.Error}
	})
	if err != nil {
		return result, fmt.Errorf("svc.Register: %w", err)
	}

	if !reg.CreateDags {
		return result, nil
	}

	// даг-инстанс на каждый валидный шаблон, у которого их ещё нет: имя
	// совпадает с именем дага в образе
	existing, err := u.dags.ListByProject(ctx, reg.ProjectName)
	if err != nil {
		return result, fmt.Errorf("dags.ListByProject: %w", err)
	}
	byTemplate := lo.CountValuesBy(existing, func(d *dagModel.Main) string { return d.Template })

	for i, t := range templates {
		if t.Manifest == nil || byTemplate[t.Name] > 0 {
			continue
		}
		ref := dagModel.NewRef(reg.ProjectName, t.Name)
		if _, err = u.dags.Create(ctx, ref, t.Name); err != nil {
			result[i].Error = fmt.Sprintf("не удалось завести даг: %v", err)
			continue
		}
		result[i].Created = true
	}

	return result, nil
}

func (u *Usecase) SetAutoUpdate(ctx context.Context, name string, autoUpdate bool) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireProject(ctx, name); err != nil {
		return err
	}
	if err := u.svc.SetAutoUpdate(ctx, name, autoUpdate); err != nil {
		return fmt.Errorf("svc.SetAutoUpdate: %w", err)
	}
	return nil
}

// Delete удаляет проект вместе с шаблонами и всеми его дагами — только
// admin: это разом уносит расписания и настройки нескольких дагов.
func (u *Usecase) Delete(ctx context.Context, name string) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireAdmin(ctx); err != nil {
		return err
	}
	if err := u.svc.Delete(ctx, name); err != nil {
		return fmt.Errorf("svc.Delete: %w", err)
	}
	return nil
}

func (u *Usecase) GetRegistration(ctx context.Context, id string) (*projectregModel.Main, error) {
	if id == "" {
		return nil, errs.IdRequired
	}
	result, err := u.registrations.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("registrations.Get: %w", err)
	}
	return result, nil
}

func (u *Usecase) ListRegistrations(ctx context.Context, req *projectregModel.ListReq) ([]*projectregModel.Main, error) {
	items, err := u.registrations.List(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("registrations.List: %w", err)
	}
	return items, nil
}

// PushCatalog — приём каталога образа от describe-Job'а: доставка
// ожидающей регистрации по одноразовому describe_id.
func (u *Usecase) PushCatalog(_ context.Context, id string, catalog []byte, errMsg string) error {
	if id == "" {
		return errs.IdRequired
	}
	if len(catalog) > maxCatalogSize {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("catalog too large: %d bytes", len(catalog))}
	}

	if u.sink == nil || !u.sink.Deliver(id, catalog, errMsg) {
		return errs.ErrFull{Err: errs.ObjectNotFound, Desc: "unknown describe id"}
	}
	return nil
}
