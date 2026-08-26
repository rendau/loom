// Package service — переменные для env-инъекции в поды тасков: как секреты,
// но значения хранятся открыто и видны в админке. Скоупы трёхуровневые
// (глобальный → проект → даг); более узкий перекрывает более широкий при
// резолве в Launch.
package service

import (
	"context"
	"fmt"
	"regexp"

	"github.com/samber/lo"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/domain/variable/model"
	"github.com/rendau/loom/server/internal/errs"
)

// nameRe — допустимые имена переменных; согласовано с манифестами SDK.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

type Service struct {
	repoDb RepoDbI
}

func New(repoDb RepoDbI) *Service {
	return &Service{repoDb: repoDb}
}

// List — переменные со значениями; scope nil — все скоупы, иначе только
// указанный.
func (s *Service) List(ctx context.Context, scope *commonModel.Scope) ([]*model.Main, error) {
	items, err := s.repoDb.List(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, nil
}

// Set создаёт переменную или перезаписывает значение существующей.
func (s *Service) Set(ctx context.Context, scope commonModel.Scope, name, value string) error {
	if !scope.Valid() {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "некорректный скоуп"}
	}
	if !nameRe.MatchString(name) {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("недопустимое имя переменной %q", name)}
	}
	if len(value) > model.MaxValueSize {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("значение больше лимита %d байт", model.MaxValueSize)}
	}

	if err := s.repoDb.Set(ctx, scope, name, value); err != nil {
		return fmt.Errorf("repoDb.Set: %w", err)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, scope commonModel.Scope, name string) error {
	found, err := s.repoDb.Delete(ctx, scope, name)
	if err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	if !found {
		return errs.VariableNotFound
	}
	return nil
}

// ResolveValues возвращает значения переменных для инъекции в env попытки
// дага (даг перекрывает проект, проект — глобальный); любая отсутствующая
// переменная — ошибка (попытка не должна стартовать с пустой переменной).
func (s *Service) ResolveValues(ctx context.Context, scope commonModel.Scope, names []string) (map[string]model.Resolved, error) {
	values, err := s.repoDb.GetValues(ctx, scope, names)
	if err != nil {
		return nil, fmt.Errorf("repoDb.GetValues: %w", err)
	}

	missing := lo.Filter(names, func(n string, _ int) bool { _, ok := values[n]; return !ok })
	if len(missing) > 0 {
		return nil, errs.ErrFull{Err: errs.VariableNotFound, Desc: fmt.Sprintf("переменные не найдены: %v", missing)}
	}
	return values, nil
}
