// Package service — переменные для env-инъекции в поды тасков: как секреты,
// но значения хранятся открыто и видны в админке. Скоупы: dag_name = "" —
// глобальный, иначе локальный для дага; локальный перекрывает глобальный
// при резолве в Launch.
package service

import (
	"context"
	"fmt"
	"regexp"

	"github.com/samber/lo"

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

// List — переменные со значениями; dagName nil — все скоупы, "" — только
// глобальные, имя дага — только его локальные.
func (s *Service) List(ctx context.Context, dagName *string) ([]*model.Main, error) {
	items, err := s.repoDb.List(ctx, dagName)
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, nil
}

// Set создаёт переменную или перезаписывает значение существующей.
func (s *Service) Set(ctx context.Context, dagName, name, value string) error {
	if !nameRe.MatchString(name) {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("недопустимое имя переменной %q", name)}
	}
	if len(value) > model.MaxValueSize {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("значение больше лимита %d байт", model.MaxValueSize)}
	}

	if err := s.repoDb.Set(ctx, dagName, name, value); err != nil {
		return fmt.Errorf("repoDb.Set: %w", err)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, dagName, name string) error {
	found, err := s.repoDb.Delete(ctx, dagName, name)
	if err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	if !found {
		return errs.VariableNotFound
	}
	return nil
}

// ResolveValues возвращает значения переменных для инъекции в env попытки
// дага (локальный скоуп перекрывает глобальный); любая отсутствующая
// переменная — ошибка (попытка не должна стартовать с пустой переменной).
func (s *Service) ResolveValues(ctx context.Context, dagName string, names []string) (map[string]string, error) {
	values, err := s.repoDb.GetValues(ctx, dagName, names)
	if err != nil {
		return nil, fmt.Errorf("repoDb.GetValues: %w", err)
	}

	missing := lo.Filter(names, func(n string, _ int) bool { _, ok := values[n]; return !ok })
	if len(missing) > 0 {
		return nil, errs.ErrFull{Err: errs.VariableNotFound, Desc: fmt.Sprintf("переменные не найдены: %v", missing)}
	}
	return values, nil
}
