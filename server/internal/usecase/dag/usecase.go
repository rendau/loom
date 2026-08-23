package dag

import (
	"context"
	"fmt"

	"github.com/samber/lo"

	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	"github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

// maxManifestSize — предохранитель на манифест из PushDagManifest.
const maxManifestSize = 1 << 20

type Usecase struct {
	svc       ServiceI
	inspector ImageInspectorI
	pools     PoolCheckerI
	sink      ManifestSinkI // nil — регистрация не через describe-Job (EXECUTOR != k8s)
}

func New(svc ServiceI, inspector ImageInspectorI, pools PoolCheckerI, sink ManifestSinkI) *Usecase {
	return &Usecase{svc: svc, inspector: inspector, pools: pools, sink: sink}
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

// Register регистрирует даг по url docker-образа: инспекция (пиннинг digest
// + `describe`) → валидация манифеста → сохранение. Имя дага берётся из
// манифеста; повторная регистрация обновляет образ и манифест. autoUpdate
// nil — сохранить текущее значение флага.
func (u *Usecase) Register(ctx context.Context, image string, autoUpdate *bool) (*model.Main, error) {
	if image == "" {
		return nil, errs.ImageRequired
	}

	digest, raw, err := u.inspector.Inspect(ctx, image)
	if err != nil {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: err.Error()}
	}

	m, err := manifest.Parse(raw)
	if err != nil {
		return nil, errs.ErrFull{Err: errs.InvalidManifest, Desc: err.Error()}
	}

	// пулы манифеста должны существовать: таск с неизвестным пулом навсегда
	// завис бы в очереди
	pools := lo.Uniq(lo.FilterMap(m.Tasks, func(t model.Task, _ int) (string, bool) {
		return t.Pool, t.Pool != ""
	}))
	if err = u.pools.CheckExist(ctx, pools); err != nil {
		return nil, err
	}

	result, err := u.svc.Register(ctx, image, digest, raw, m, autoUpdate)
	if err != nil {
		return nil, fmt.Errorf("svc.Register: %w", err)
	}
	return result, nil
}

// PushManifest — приём манифеста от describe-Job'а: доставка
// ожидающей регистрации по одноразовому describe_id.
func (u *Usecase) PushManifest(_ context.Context, id string, manifest []byte, errMsg string) error {
	if id == "" {
		return errs.IdRequired
	}
	if len(manifest) > maxManifestSize {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("manifest too large: %d bytes", len(manifest))}
	}

	if u.sink == nil || !u.sink.Deliver(id, manifest, errMsg) {
		return errs.ErrFull{Err: errs.ObjectNotFound, Desc: "unknown describe id"}
	}
	return nil
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

func (u *Usecase) SetAutoUpdate(ctx context.Context, name string, autoUpdate bool) error {
	if name == "" {
		return errs.IdRequired
	}
	if err := u.svc.SetAutoUpdate(ctx, name, autoUpdate); err != nil {
		return fmt.Errorf("svc.SetAutoUpdate: %w", err)
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
