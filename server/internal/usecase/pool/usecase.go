package pool

import (
	"context"
	"fmt"

	"github.com/rendau/loom/server/internal/domain/pool/model"
	"github.com/rendau/loom/server/internal/errs"
)

type Usecase struct {
	svc ServiceI
}

func New(svc ServiceI) *Usecase {
	return &Usecase{svc: svc}
}

func (u *Usecase) List(ctx context.Context) ([]*model.Main, error) {
	items, err := u.svc.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("svc.List: %w", err)
	}
	return items, nil
}

func (u *Usecase) Set(ctx context.Context, name string, slots int) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.svc.Set(ctx, name, slots); err != nil {
		return fmt.Errorf("svc.Set: %w", err)
	}
	return nil
}
