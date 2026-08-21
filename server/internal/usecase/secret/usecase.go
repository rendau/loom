package secret

import (
	"context"
	"fmt"

	"github.com/rendau/loom/server/internal/domain/secret/model"
	"github.com/rendau/loom/server/internal/errs"
)

type Usecase struct {
	svc ServiceI
}

func New(svc ServiceI) *Usecase {
	return &Usecase{svc: svc}
}

func (u *Usecase) List(ctx context.Context) ([]*model.Meta, error) {
	items, err := u.svc.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("svc.List: %w", err)
	}
	return items, nil
}

func (u *Usecase) Set(ctx context.Context, name string, value []byte) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.svc.Set(ctx, name, value); err != nil {
		return fmt.Errorf("svc.Set: %w", err)
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
