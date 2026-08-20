package db

import (
	"context"
	"fmt"

	"github.com/mechta-market/mobone/v2"
	moboneTools "github.com/mechta-market/mobone/v2/tools"
	"github.com/samber/lo"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/dag/model"
	repoModel "github.com/rendau/loom/server/internal/domain/dag/repo/db/model"
)

type Repo struct {
	*commonRepoPg.Base
	ModelStore *mobone.ModelStore
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{
		Base: base,
		ModelStore: &mobone.ModelStore{
			Con:                base.Con,
			TransactionManager: base.TxM,
			QB:                 base.QB,
			TableName:          "dag",
		},
	}
}

func (r *Repo) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	conditions, conditionExps := r.getConditions(pars)
	sort := moboneTools.ConstructSortColumns(allowedSortFields, pars.Sort)
	items := make([]*repoModel.Select, 0)

	totalCount, err := r.ModelStore.List(ctx, mobone.ListParams{
		Conditions:           conditions,
		ConditionExpressions: conditionExps,
		Page:                 pars.Page,
		PageSize:             pars.PageSize,
		WithTotalCount:       pars.WithTotalCount,
		OnlyCount:            pars.OnlyCount,
		Sort:                 sort,
	}, func(add bool) mobone.ListModelI {
		item := &repoModel.Select{}
		if add {
			items = append(items, item)
		}
		return item
	})
	if err != nil {
		return nil, 0, fmt.Errorf("ModelStore.List: %w", err)
	}
	return lo.Map(items, repoModel.EncodeSelect), totalCount, nil
}

func (r *Repo) Get(ctx context.Context, name string) (*model.Main, bool, error) {
	m := &repoModel.Select{Name: name}
	found, err := r.ModelStore.Get(ctx, m)
	if err != nil {
		return nil, false, fmt.Errorf("ModelStore.Get: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	return repoModel.EncodeSelect(m, 0), true, nil
}

func (r *Repo) UpdateOrCreate(ctx context.Context, name string, obj *model.Edit) error {
	m := repoModel.DecodeUpsert(obj)
	m.PKName = name
	if err := r.ModelStore.UpdateOrCreate(ctx, m); err != nil {
		return fmt.Errorf("ModelStore.UpdateOrCreate: %w", err)
	}
	return nil
}

func (r *Repo) Update(ctx context.Context, name string, obj *model.Edit) error {
	m := repoModel.DecodeUpsert(obj)
	m.PKName = name
	if err := r.ModelStore.Update(ctx, m); err != nil {
		return fmt.Errorf("ModelStore.Update: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, name string) error {
	m := &repoModel.Upsert{PKName: name}
	if err := r.ModelStore.Delete(ctx, m); err != nil {
		return fmt.Errorf("ModelStore.Delete: %w", err)
	}
	return nil
}
