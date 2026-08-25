package service

import (
	"context"
	"fmt"
	"regexp"

	"github.com/rendau/loom/server/internal/domain/pool/model"
	"github.com/rendau/loom/server/internal/errs"
)

// nameRe — допустимые имена пулов; согласовано с именами в манифестах.
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

type Service struct {
	repoDb RepoDbI
}

func New(repoDb RepoDbI) *Service {
	return &Service{repoDb: repoDb}
}

func (s *Service) List(ctx context.Context) ([]*model.Main, error) {
	items, err := s.repoDb.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, nil
}

// Set создаёт пул или меняет число слотов существующего. Удаления пулов
// нет: на пул могут ссылаться манифесты дагов и ранов; slots = 0
// ставит пул на паузу.
func (s *Service) Set(ctx context.Context, name string, slots int) error {
	if !nameRe.MatchString(name) {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("недопустимое имя пула %q", name)}
	}
	if slots < 0 || slots > model.MaxSlots {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("slots вне диапазона [0, %d]", model.MaxSlots)}
	}

	if err := s.repoDb.Set(ctx, name, slots); err != nil {
		return fmt.Errorf("repoDb.Set: %w", err)
	}
	return nil
}

// CheckExist проверяет существование пулов (валидация манифеста при
// регистрации дага: неизвестный пул оставил бы таски в очереди навсегда).
func (s *Service) CheckExist(ctx context.Context, names []string) error {
	if len(names) == 0 {
		return nil
	}

	missing, err := s.repoDb.ListMissing(ctx, names)
	if err != nil {
		return fmt.Errorf("repoDb.ListMissing: %w", err)
	}
	if len(missing) > 0 {
		return errs.ErrFull{Err: errs.PoolNotFound,
			Desc: fmt.Sprintf("создайте пулы заранее: %v", missing)}
	}
	return nil
}
