package secret

import (
	"context"
	"fmt"
	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/domain/secret/model"
	"github.com/rendau/loom/server/internal/errs"
)

type Usecase struct {
	svc   ServiceI
	authz AuthzI
}

func New(svc ServiceI, authz AuthzI) *Usecase {
	return &Usecase{svc: svc, authz: authz}
}

func (u *Usecase) List(ctx context.Context, scope *commonModel.Scope) ([]*model.Meta, error) {
	items, err := u.svc.List(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("svc.List: %w", err)
	}
	return items, nil
}

func (u *Usecase) Set(ctx context.Context, scope commonModel.Scope, name string, value []byte) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireScope(ctx, scope); err != nil {
		return err
	}
	if err := u.svc.Set(ctx, scope, name, value); err != nil {
		return fmt.Errorf("svc.Set: %w", err)
	}
	return nil
}

func (u *Usecase) Delete(ctx context.Context, scope commonModel.Scope, name string) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireScope(ctx, scope); err != nil {
		return err
	}
	if err := u.svc.Delete(ctx, scope, name); err != nil {
		return fmt.Errorf("svc.Delete: %w", err)
	}
	return nil
}

// Move переносит запись в другой скоуп. Права нужны на оба: это разом и
// удаление из старого места, и создание в новом.
func (u *Usecase) Move(ctx context.Context, from, to commonModel.Scope, name string) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireScope(ctx, from); err != nil {
		return err
	}
	if err := u.authz.RequireScope(ctx, to); err != nil {
		return err
	}
	if err := u.svc.Move(ctx, from, to, name); err != nil {
		return fmt.Errorf("svc.Move: %w", err)
	}
	return nil
}

// GetValue — расшифрованное значение секрета («посмотреть по кнопке»):
// глобальные видит только admin, локальные — ещё и владелец дага.
func (u *Usecase) GetValue(ctx context.Context, scope commonModel.Scope, name string) ([]byte, error) {
	if name == "" {
		return nil, errs.IdRequired
	}
	if err := u.authz.RequireScope(ctx, scope); err != nil {
		return nil, err
	}
	value, err := u.svc.GetValue(ctx, scope, name)
	if err != nil {
		return nil, fmt.Errorf("svc.GetValue: %w", err)
	}
	return value, nil
}
