// Package service (project) — проекты: docker-образ и объявленные в нём
// даги («шаблоны»). Регистрация образа обновляет проект и его каталог
// целиком; даги-инстансы заводятся отдельно (домен dag).
package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"

	dagManifest "github.com/rendau/loom/server/internal/domain/dag/manifest"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/project/model"
	"github.com/rendau/loom/server/internal/errs"
)

type Service struct {
	repoDb RepoDbI
}

func New(repoDb RepoDbI) *Service {
	return &Service{repoDb: repoDb}
}

func (s *Service) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	items, tCount, err := s.repoDb.List(ctx, pars)
	if err != nil {
		return nil, 0, fmt.Errorf("repoDb.List: %w", err)
	}

	counts, err := s.repoDb.CountDags(ctx,
		lo.Map(items, func(v *model.Main, _ int) string { return v.Name }))
	if err != nil {
		return nil, 0, fmt.Errorf("repoDb.CountDags: %w", err)
	}
	for _, p := range items {
		p.DagCount = counts[p.Name]
	}

	return items, tCount, nil
}

func (s *Service) Get(ctx context.Context, name string, errNE bool) (*model.Main, bool, error) {
	result, found, err := s.repoDb.Get(ctx, name)
	if err != nil {
		return nil, false, fmt.Errorf("repoDb.Get: %w", err)
	}
	if !found {
		if errNE {
			return nil, false, errs.ProjectNotFound
		}
		return nil, false, nil
	}
	return result, found, nil
}

// Register сохраняет проект по каталогу образа: валидирует манифесты дагов
// и переписывает каталог (шаблоны). Ошибка отдельного дага не отменяет
// регистрацию остальных — она возвращается в результате по шаблонам;
// регистрация падает, только если валидных дагов не осталось совсем.
func (s *Service) Register(ctx context.Context, spec model.RegisterSpec,
	catalog *dagModel.Catalog,
) (*model.Main, []model.TemplateEdit, error) {
	name := spec.Name
	if !dagManifest.ValidName(name) {
		return nil, nil, errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("недопустимое имя проекта %q", name)}
	}
	if err := dagManifest.ValidateCatalog(catalog); err != nil {
		return nil, nil, err
	}

	items := lo.Map(catalog.Dags, func(d dagModel.CatalogDag, _ int) model.TemplateEdit {
		item := model.TemplateEdit{Name: d.Name, SdkVersion: catalog.SdkVersion, Error: d.Error}
		switch {
		case item.Error != "":
			// даг не собрался на стороне SDK — его ошибка уже в каталоге
		case d.Manifest == nil:
			item.Error = "манифест отсутствует"
		case d.Manifest.Name != d.Name:
			item.Error = fmt.Sprintf("имя в манифесте (%q) не совпадает с именем в каталоге", d.Manifest.Name)
		default:
			if err := dagManifest.Validate(d.Manifest); err != nil {
				item.Error = err.Error()
			} else {
				item.Manifest = d.Raw
			}
		}
		return item
	})

	broken := lo.Filter(items, func(v model.TemplateEdit, _ int) bool { return v.Manifest == nil })
	if len(broken) == len(items) {
		return nil, items, errs.ErrFull{Err: errs.InvalidManifest,
			Desc: strings.Join(lo.Map(broken, func(v model.TemplateEdit, _ int) string {
				return fmt.Sprintf("даг %q: %s", v.Name, v.Error)
			}), "; ")}
	}

	edit := &model.Edit{
		Image:       &spec.Image,
		ImageDigest: &spec.ImageDigest,
		AutoUpdate:  spec.AutoUpdate,
		ModifiedAt:  new(time.Now()),
	}
	if spec.ImageSizeBytes > 0 {
		edit.ImageSizeBytes = &spec.ImageSizeBytes
	}

	err := s.repoDb.UpdateOrCreate(ctx, name, edit)
	if err != nil {
		return nil, items, fmt.Errorf("repoDb.UpdateOrCreate: %w", err)
	}

	if err = s.repoDb.SetTemplates(ctx, name, items); err != nil {
		return nil, items, fmt.Errorf("repoDb.SetTemplates: %w", err)
	}

	result, _, err := s.Get(ctx, name, true)
	return result, items, err
}

// SetAutoUpdate включает/выключает poll-синк новой версии образа проекта:
// флаг общий на все его даги.
func (s *Service) SetAutoUpdate(ctx context.Context, name string, autoUpdate bool) error {
	if _, _, err := s.Get(ctx, name, true); err != nil {
		return err
	}

	err := s.repoDb.Update(ctx, name, &model.Edit{
		AutoUpdate: &autoUpdate,
		ModifiedAt: new(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("repoDb.Update: %w", err)
	}
	return nil
}

// ListAutoUpdate возвращает проекты с включённым авто-обновлением —
// кандидатов dagsync-цикла.
func (s *Service) ListAutoUpdate(ctx context.Context) ([]*model.Main, error) {
	items, _, err := s.repoDb.List(ctx, &model.ListReq{AutoUpdate: new(true)})
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, nil
}

// Delete удаляет проект вместе с его шаблонами и дагами (каскад в БД).
func (s *Service) Delete(ctx context.Context, name string) error {
	if _, _, err := s.Get(ctx, name, true); err != nil {
		return err
	}

	if err := s.repoDb.Delete(ctx, name); err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	return nil
}

// ── шаблоны ─────────────────────────────────────────────────────────────

func (s *Service) ListTemplates(ctx context.Context, project string) ([]*model.Template, error) {
	items, err := s.repoDb.ListTemplates(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListTemplates: %w", err)
	}
	return items, nil
}

func (s *Service) GetTemplate(ctx context.Context, project, name string, errNE bool) (*model.Template, bool, error) {
	result, found, err := s.repoDb.GetTemplate(ctx, project, name)
	if err != nil {
		return nil, false, fmt.Errorf("repoDb.GetTemplate: %w", err)
	}
	if !found {
		if errNE {
			return nil, false, errs.ErrFull{Err: errs.TemplateNotFound,
				Desc: fmt.Sprintf("в образе проекта %q нет дага %q", project, name)}
		}
		return nil, false, nil
	}
	return result, found, nil
}
