package service

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/samber/lo"

	"github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/errs"
)

// nameRe — допустимые имена дагов и тасков; согласовано с ограничениями
// artifact-сервера (streamstore ref) и лейблов kubernetes.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

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
	return items, tCount, nil
}

func (s *Service) Get(ctx context.Context, name string, errNE bool) (*model.Main, bool, error) {
	result, found, err := s.repoDb.Get(ctx, name)
	if err != nil {
		return nil, false, fmt.Errorf("repoDb.Get: %w", err)
	}
	if !found {
		if errNE {
			return nil, false, errs.DagNotFound
		}
		return nil, false, nil
	}
	return result, found, nil
}

// Register сохраняет даг по манифесту, полученному из образа: валидирует
// манифест и создаёт/обновляет запись (перерегистрация = новая версия
// образа). Paused при перерегистрации не трогается.
func (s *Service) Register(ctx context.Context, image, imageDigest string, rawManifest []byte, m *model.Manifest) (*model.Main, error) {
	if err := ValidateManifest(m); err != nil {
		return nil, err
	}

	err := s.repoDb.UpdateOrCreate(ctx, m.Name, &model.Edit{
		Image:       &image,
		ImageDigest: &imageDigest,
		Schedule:    &m.Schedule,
		Manifest:    &rawManifest,
		ModifiedAt:  new(time.Now()),
	})
	if err != nil {
		return nil, fmt.Errorf("repoDb.UpdateOrCreate: %w", err)
	}

	result, _, err := s.Get(ctx, m.Name, true)
	return result, err
}

func (s *Service) SetPaused(ctx context.Context, name string, paused bool) error {
	if _, _, err := s.Get(ctx, name, true); err != nil {
		return err
	}

	err := s.repoDb.Update(ctx, name, &model.Edit{
		Paused:     &paused,
		ModifiedAt: new(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("repoDb.Update: %w", err)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	if _, _, err := s.Get(ctx, name, true); err != nil {
		return err
	}

	if err := s.repoDb.Delete(ctx, name); err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	return nil
}

// ValidateManifest проверяет манифест дага: имена, целостность рёбер и
// ацикличность. Манифест приходит из чужого образа — не доверяем ему,
// даже если SDK валидировал даг на своей стороне.
func ValidateManifest(m *model.Manifest) error {
	fail := func(desc string) error {
		return errs.ErrFull{Err: errs.InvalidManifest, Desc: desc}
	}

	if m == nil {
		return fail("манифест пуст")
	}
	if m.SdkVersion == "" {
		return fail("отсутствует sdk_version")
	}
	if !nameRe.MatchString(m.Name) {
		return fail(fmt.Sprintf("недопустимое имя дага %q", m.Name))
	}
	if len(m.Tasks) == 0 {
		return fail("в даге нет тасков")
	}

	tasks := map[string]model.Task{}
	for _, t := range m.Tasks {
		if !nameRe.MatchString(t.Name) {
			return fail(fmt.Sprintf("недопустимое имя таска %q", t.Name))
		}
		if _, ok := tasks[t.Name]; ok {
			return fail(fmt.Sprintf("дубль таска %q", t.Name))
		}
		tasks[t.Name] = t
	}

	for _, t := range m.Tasks {
		seen := map[string]bool{}
		for _, dep := range t.DependsOn {
			if dep.Task == t.Name {
				return fail(fmt.Sprintf("таск %q зависит от самого себя", t.Name))
			}
			if _, ok := tasks[dep.Task]; !ok {
				return fail(fmt.Sprintf("таск %q зависит от неизвестного таска %q", t.Name, dep.Task))
			}
			if seen[dep.Task] {
				return fail(fmt.Sprintf("таск %q: дубль зависимости %q", t.Name, dep.Task))
			}
			seen[dep.Task] = true
		}
	}

	// ацикличность — алгоритм Кана
	inDegree := lo.MapEntries(tasks, func(name string, t model.Task) (string, int) {
		return name, len(t.DependsOn)
	})
	queue := lo.Keys(lo.PickBy(inDegree, func(_ string, deg int) bool { return deg == 0 }))
	visited := 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		visited++
		for _, t := range m.Tasks {
			if lo.ContainsBy(t.DependsOn, func(d model.Dep) bool { return d.Task == cur }) {
				inDegree[t.Name]--
				if inDegree[t.Name] == 0 {
					queue = append(queue, t.Name)
				}
			}
		}
	}
	if visited != len(tasks) {
		return fail("граф тасков содержит цикл")
	}

	return nil
}
