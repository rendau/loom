package secret

import (
	"context"
	"fmt"

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

func (u *Usecase) List(ctx context.Context, dagName *string) ([]*model.Meta, error) {
	items, err := u.svc.List(ctx, dagName)
	if err != nil {
		return nil, fmt.Errorf("svc.List: %w", err)
	}
	return items, nil
}

func (u *Usecase) Set(ctx context.Context, dagName, name string, value []byte) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireScope(ctx, dagName); err != nil {
		return err
	}
	if err := u.svc.Set(ctx, dagName, name, value); err != nil {
		return fmt.Errorf("svc.Set: %w", err)
	}
	return nil
}

func (u *Usecase) Delete(ctx context.Context, dagName, name string) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.authz.RequireScope(ctx, dagName); err != nil {
		return err
	}
	if err := u.svc.Delete(ctx, dagName, name); err != nil {
		return fmt.Errorf("svc.Delete: %w", err)
	}
	return nil
}

// GetValue — расшифрованное значение секрета («посмотреть по кнопке»):
// глобальные видит только admin, локальные — ещё и владелец дага.
func (u *Usecase) GetValue(ctx context.Context, dagName, name string) ([]byte, error) {
	if name == "" {
		return nil, errs.IdRequired
	}
	if err := u.authz.RequireScope(ctx, dagName); err != nil {
		return nil, err
	}
	value, err := u.svc.GetValue(ctx, dagName, name)
	if err != nil {
		return nil, fmt.Errorf("svc.GetValue: %w", err)
	}
	return value, nil
}
