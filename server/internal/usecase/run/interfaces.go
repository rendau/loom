package run

import (
	"context"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/run/model"
)

type ServiceI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, id string, errNE bool) (*model.Main, bool, error)
	GetDetails(ctx context.Context, id string) (*model.Main, []dagModel.Task, []*model.TaskInstance, []*model.Attempt, error)
	Trigger(ctx context.Context, dag *dagModel.Main, spec model.TriggerSpec) (string, error)
	RetryTask(ctx context.Context, runId, task string) error
	Cancel(ctx context.Context, runId string) ([]model.AttemptRef, error)
	PushValue(ctx context.Context, ref model.AttemptRef, key string, value []byte) error
	PullValue(ctx context.Context, runId, task, key string) (*model.TaskValue, error)
	ListValues(ctx context.Context, runId string) ([]*model.TaskValue, error)
}

type DagServiceI interface {
	Get(ctx context.Context, name string, errNE bool) (*dagModel.Main, bool, error)
}

// AuthzI — проверка прав вызывающего на даг (триггер, backfill, ретрай).
type AuthzI interface {
	RequireDag(ctx context.Context, dagName string) error
}

// SchedulerI — операции планировщика, нужные ручкам: тычок (не ждать тика
// после триггера рана) и добивание живых попыток остановленного рана (kill
// через executor + финализация лога и артефактов).
type SchedulerI interface {
	Nudge()
	CancelAttempts(ctx context.Context, refs []model.AttemptRef)
}
