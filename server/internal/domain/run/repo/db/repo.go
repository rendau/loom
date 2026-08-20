package db

import (
	"context"
	"fmt"

	"github.com/mechta-market/mobone/v2"
	moboneTools "github.com/mechta-market/mobone/v2/tools"
	"github.com/samber/lo"

	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/run/model"
	repoModel "github.com/rendau/loom/server/internal/domain/run/repo/db/model"
)

// Repo — хранилище рана и его детей: run, task_instance, attempt живут
// одной доменной областью и меняются в общих транзакциях планировщика.
type Repo struct {
	*commonRepoPg.Base
	RunStore     *mobone.ModelStore
	TaskStore    *mobone.ModelStore
	AttemptStore *mobone.ModelStore
}

func New(base *commonRepoPg.Base) *Repo {
	newStore := func(table string) *mobone.ModelStore {
		return &mobone.ModelStore{
			Con:                base.Con,
			TransactionManager: base.TxM,
			QB:                 base.QB,
			TableName:          table,
		}
	}

	return &Repo{
		Base:         base,
		RunStore:     newStore("run"),
		TaskStore:    newStore("task_instance"),
		AttemptStore: newStore("attempt"),
	}
}

// ── run ─────────────────────────────────────────────────

func (r *Repo) ListRuns(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	conditions, conditionExps := r.getConditions(pars)
	sort := moboneTools.ConstructSortColumns(allowedSortFields, pars.Sort)
	items := make([]*repoModel.RunSelect, 0)

	totalCount, err := r.RunStore.List(ctx, mobone.ListParams{
		Conditions:           conditions,
		ConditionExpressions: conditionExps,
		Page:                 pars.Page,
		PageSize:             pars.PageSize,
		WithTotalCount:       pars.WithTotalCount,
		OnlyCount:            pars.OnlyCount,
		Sort:                 sort,
	}, func(add bool) mobone.ListModelI {
		item := &repoModel.RunSelect{}
		if add {
			items = append(items, item)
		}
		return item
	})
	if err != nil {
		return nil, 0, fmt.Errorf("RunStore.List: %w", err)
	}
	return lo.Map(items, repoModel.EncodeRunSelect), totalCount, nil
}

func (r *Repo) GetRun(ctx context.Context, id string) (*model.Main, bool, error) {
	m := &repoModel.RunSelect{Id: id}
	found, err := r.RunStore.Get(ctx, m)
	if err != nil {
		return nil, false, fmt.Errorf("RunStore.Get: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	return repoModel.EncodeRunSelect(m, 0), true, nil
}

func (r *Repo) CreateRun(ctx context.Context, obj *model.Main) error {
	if err := r.RunStore.Create(ctx, repoModel.DecodeRunCreate(obj)); err != nil {
		return fmt.Errorf("RunStore.Create: %w", err)
	}
	return nil
}

func (r *Repo) UpdateRun(ctx context.Context, id string, obj *model.Edit) error {
	m := repoModel.DecodeRunUpsert(obj)
	m.PKId = id
	if err := r.RunStore.Update(ctx, m); err != nil {
		return fmt.Errorf("RunStore.Update: %w", err)
	}
	return nil
}

// ── task_instance ───────────────────────────────────────

// CreateTaskInstances заводит task instance'ы рана в статусе pending.
func (r *Repo) CreateTaskInstances(ctx context.Context, runId string, tasks []string) error {
	models := lo.Map(tasks, func(task string, _ int) mobone.CreateModelI {
		return &repoModel.TaskUpsert{
			PKRunId: runId,
			PKTask:  task,
			Status:  new(model.TaskStatusPending),
			Attempt: new(int32(0)),
		}
	})
	if err := r.TaskStore.CreateMany(ctx, models); err != nil {
		return fmt.Errorf("TaskStore.CreateMany: %w", err)
	}
	return nil
}

func (r *Repo) ListTaskInstances(ctx context.Context, runId string) ([]*model.TaskInstance, error) {
	items := make([]*repoModel.TaskSelect, 0)

	_, err := r.TaskStore.List(ctx, mobone.ListParams{
		Conditions: map[string]any{"run_id": runId},
	}, func(add bool) mobone.ListModelI {
		item := &repoModel.TaskSelect{}
		if add {
			items = append(items, item)
		}
		return item
	})
	if err != nil {
		return nil, fmt.Errorf("TaskStore.List: %w", err)
	}
	return lo.Map(items, repoModel.EncodeTaskSelect), nil
}

// ── attempt ─────────────────────────────────────────────

func (r *Repo) CreateAttempt(ctx context.Context, ref model.AttemptRef) error {
	m := &repoModel.AttemptUpsert{
		PKRunId:   ref.RunId,
		PKTask:    ref.Task,
		PKAttempt: ref.Attempt,
		Status:    new(model.AttemptStatusStarting),
	}
	if err := r.AttemptStore.Create(ctx, m); err != nil {
		return fmt.Errorf("AttemptStore.Create: %w", err)
	}
	return nil
}

func (r *Repo) GetAttempt(ctx context.Context, ref model.AttemptRef) (*model.Attempt, bool, error) {
	m := &repoModel.AttemptSelect{RunId: ref.RunId, Task: ref.Task, Attempt: ref.Attempt}
	found, err := r.AttemptStore.Get(ctx, m)
	if err != nil {
		return nil, false, fmt.Errorf("AttemptStore.Get: %w", err)
	}
	if !found {
		return nil, false, nil
	}
	return repoModel.EncodeAttemptSelect(m, 0), true, nil
}

func (r *Repo) ListAttempts(ctx context.Context, runId string) ([]*model.Attempt, error) {
	items := make([]*repoModel.AttemptSelect, 0)

	_, err := r.AttemptStore.List(ctx, mobone.ListParams{
		Conditions: map[string]any{"run_id": runId},
	}, func(add bool) mobone.ListModelI {
		item := &repoModel.AttemptSelect{}
		if add {
			items = append(items, item)
		}
		return item
	})
	if err != nil {
		return nil, fmt.Errorf("AttemptStore.List: %w", err)
	}
	return lo.Map(items, repoModel.EncodeAttemptSelect), nil
}
