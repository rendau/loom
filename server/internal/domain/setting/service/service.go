// Package service — настройки инсталляции в БД (вместо env-конфига):
// retention ранов, TTL k8s Job'ов и т.п. Скоуп как у переменных:
// глобальный → проект → даг, более узкий перекрывает более широкий при
// резолве. Имена настроек фиксированы (model.Defs) — произвольные
// отклоняются, поэтому потребители всегда получают полный набор значений
// (страховка — дефолт из Defs).
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/domain/setting/model"
	"github.com/rendau/loom/server/internal/errs"
)

type Service struct {
	repoDb RepoDbI
}

func New(repoDb RepoDbI) *Service {
	return &Service{repoDb: repoDb}
}

// List — сохранённые значения; scope nil — все скоупы, иначе только
// указанный.
func (s *Service) List(ctx context.Context, scope *commonModel.Scope) ([]*model.Main, error) {
	items, err := s.repoDb.List(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, nil
}

// Set задаёт значение настройки в скоупе, валидируя имя и значение по типу.
func (s *Service) Set(ctx context.Context, scope commonModel.Scope, name, value string) error {
	def, ok := model.Defs[name]
	if !ok {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("неизвестная настройка %q", name)}
	}
	if !scope.Valid() {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "некорректный скоуп"}
	}
	if !scope.IsGlobal() && !def.Scoped {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("настройка %q задаётся только глобально", name)}
	}
	if _, err := parseValue(def.Kind, value); err != nil {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("недопустимое значение настройки %q: %v", name, err)}
	}

	if err := s.repoDb.Set(ctx, scope, name, value); err != nil {
		return fmt.Errorf("repoDb.Set: %w", err)
	}
	return nil
}

// Delete удаляет уточнение настройки у проекта или дага (возврат к более
// широкому скоупу). Глобальный скоуп не удаляется — только меняется:
// retention и executor всегда должны видеть полный набор значений.
func (s *Service) Delete(ctx context.Context, scope commonModel.Scope, name string) error {
	if scope.IsGlobal() {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: "глобальное значение настройки нельзя удалить — только изменить"}
	}
	if _, ok := model.Defs[name]; !ok {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("неизвестная настройка %q", name)}
	}

	found, err := s.repoDb.Delete(ctx, scope, name)
	if err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	if !found {
		return errs.ObjectNotFound
	}
	return nil
}

// ResolveGlobal — эффективные настройки без уточнений (глобальный скоуп).
func (s *Service) ResolveGlobal(ctx context.Context) (model.Effective, error) {
	return s.Resolve(ctx, commonModel.GlobalScope())
}

// Resolve — эффективные настройки для скоупа: значение дага перекрывает
// проектное, проектное — глобальное, отсутствие всех закрывает дефолт из
// Defs.
func (s *Service) Resolve(ctx context.Context, scope commonModel.Scope) (model.Effective, error) {
	byScope, err := s.ResolveMany(ctx, []commonModel.Scope{scope})
	if err != nil {
		return model.Effective{}, err
	}
	return byScope[scope], nil
}

// ResolveMany — эффективные настройки сразу для набора скоупов одним
// запросом (retention-проход).
func (s *Service) ResolveMany(ctx context.Context, scopes []commonModel.Scope) (map[commonModel.Scope]model.Effective, error) {
	values, err := s.repoDb.GetValues(ctx, scopes)
	if err != nil {
		return nil, fmt.Errorf("repoDb.GetValues: %w", err)
	}

	result := make(map[commonModel.Scope]model.Effective, len(scopes))
	for _, scope := range scopes {
		result[scope] = buildEffective(scope, values)
	}
	return result, nil
}

// buildEffective собирает Effective, идя по цепочке скоупов от самого
// узкого к глобальному. Битое значение в БД (не должно случаться — Set
// валидирует) пропускается с warn: сработает следующий уровень.
func buildEffective(scope commonModel.Scope, values map[commonModel.Scope]map[string]string) model.Effective {
	get := func(name string) any {
		def := model.Defs[name]
		for _, sc := range scope.Chain() {
			raw, ok := values[sc][name]
			if !ok {
				continue
			}
			v, err := parseValue(def.Kind, raw)
			if err != nil {
				slog.Warn("invalid setting value in db", "name", name, "value", raw, "error", err)
				continue
			}
			return v
		}
		v, _ := parseValue(def.Kind, def.Default)
		return v
	}

	return model.Effective{
		RunTTL:      get(model.RunTTL).(time.Duration),
		RunKeepLast: get(model.RunKeepLast).(int64),
		K8sJobTTL:   get(model.K8sJobTTL).(time.Duration),
		DagRegTTL:   get(model.DagRegTTL).(time.Duration),
	}
}

// parseValue валидирует и разбирает значение по типу настройки:
// duration — Go-нотация ("720h", "0"), неотрицательная; int — целое >= 0.
func parseValue(kind, value string) (any, error) {
	switch kind {
	case model.KindDuration:
		d, err := time.ParseDuration(value)
		if err != nil {
			return nil, fmt.Errorf("ожидается длительность вида 720h/90m/0: %w", err)
		}
		if d < 0 {
			return nil, fmt.Errorf("длительность не может быть отрицательной")
		}
		return d, nil
	case model.KindInt:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("ожидается целое число: %w", err)
		}
		if n < 0 {
			return nil, fmt.Errorf("число не может быть отрицательным")
		}
		return n, nil
	default:
		return nil, fmt.Errorf("неизвестный тип настройки %q", kind)
	}
}
