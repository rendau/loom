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

// Repo читает даги из view dag_full (даг + образ проекта + манифест
// шаблона), а пишет в таблицу dag — отсюда два ModelStore.
type Repo struct {
	*commonRepoPg.Base
	ModelStore *mobone.ModelStore
	WriteStore *mobone.ModelStore
}

func New(base *commonRepoPg.Base) *Repo {
	return &Repo{
		Base: base,
		ModelStore: &mobone.ModelStore{
			Con:                base.Con,
			TransactionManager: base.TxM,
			QB:                 base.QB,
			TableName:          "dag_full",
		},
		WriteStore: &mobone.ModelStore{
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

func (r *Repo) Get(ctx context.Context, ref model.Ref) (*model.Main, bool, error) {
	m := &repoModel.Select{ProjectName: ref.Project, Name: ref.Name}
	found, err := r.ModelStore.Get(ctx, m)
	if err != nil {
		return nil, false, fmt.Errorf("ModelStore.Get: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	return repoModel.EncodeSelect(m, 0), true, nil
}

func (r *Repo) Create(ctx context.Context, ref model.Ref, obj *model.Edit) error {
	m := repoModel.DecodeUpsert(obj)
	m.PKProject, m.PKName = ref.Project, ref.Name
	if err := r.WriteStore.Create(ctx, m); err != nil {
		return fmt.Errorf("WriteStore.Create: %w", err)
	}
	return nil
}

func (r *Repo) Update(ctx context.Context, ref model.Ref, obj *model.Edit) error {
	m := repoModel.DecodeUpsert(obj)
	m.PKProject, m.PKName = ref.Project, ref.Name
	if err := r.WriteStore.Update(ctx, m); err != nil {
		return fmt.Errorf("WriteStore.Update: %w", err)
	}
	return nil
}

func (r *Repo) Delete(ctx context.Context, ref model.Ref) error {
	m := &repoModel.Upsert{PKProject: ref.Project, PKName: ref.Name}
	if err := r.WriteStore.Delete(ctx, m); err != nil {
		return fmt.Errorf("WriteStore.Delete: %w", err)
	}
	return nil
}
