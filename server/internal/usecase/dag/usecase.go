package dag

import (
	"context"
	"fmt"

	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	"github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

type Usecase struct {
	svc       ServiceI
	inspector ImageInspectorI
}

func New(svc ServiceI, inspector ImageInspectorI) *Usecase {
	return &Usecase{svc: svc, inspector: inspector}
}

func (u *Usecase) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	if err := util.RequirePageSize(pars.ListParams, 0); err != nil {
		return nil, 0, err
	}
	items, tCount, err := u.svc.List(ctx, pars)
	if err != nil {
		return nil, 0, fmt.Errorf("svc.List: %w", err)
	}
	return items, tCount, nil
}

func (u *Usecase) Get(ctx context.Context, name string) (*model.Main, error) {
	result, _, err := u.svc.Get(ctx, name, true)
	if err != nil {
		return nil, fmt.Errorf("svc.Get: %w", err)
	}
	return result, nil
}

// Register регистрирует даг по url docker-образа: pull → пиннинг digest →
// `describe` → валидация манифеста → сохранение. Имя дага берётся из
// манифеста; повторная регистрация обновляет образ и манифест.
func (u *Usecase) Register(ctx context.Context, image string) (*model.Main, error) {
	if image == "" {
		return nil, errs.ImageRequired
	}

	if err := u.inspector.Pull(ctx, image); err != nil {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: err.Error()}
	}

	digest, err := u.inspector.ResolveDigest(ctx, image)
	if err != nil {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: err.Error()}
	}

	raw, err := u.inspector.Describe(ctx, digest)
	if err != nil {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: err.Error()}
	}

	m, err := manifest.Parse(raw)
	if err != nil {
		return nil, errs.ErrFull{Err: errs.InvalidManifest, Desc: err.Error()}
	}

	result, err := u.svc.Register(ctx, image, digest, raw, m)
	if err != nil {
		return nil, fmt.Errorf("svc.Register: %w", err)
	}
	return result, nil
}

func (u *Usecase) SetPaused(ctx context.Context, name string, paused bool) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.svc.SetPaused(ctx, name, paused); err != nil {
		return fmt.Errorf("svc.SetPaused: %w", err)
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
