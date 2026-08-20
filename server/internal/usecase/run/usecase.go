package run

import (
	"context"
	"fmt"

	"github.com/rendau/loom/server/internal/domain/run/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

type Usecase struct {
	svc       ServiceI
	dagSvc    DagServiceI
	scheduler SchedulerI
}

func New(svc ServiceI, dagSvc DagServiceI, scheduler SchedulerI) *Usecase {
	return &Usecase{svc: svc, dagSvc: dagSvc, scheduler: scheduler}
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

func (u *Usecase) Get(ctx context.Context, id string) (*model.Main, []*model.TaskInstance, []*model.Attempt, error) {
	if id == "" {
		return nil, nil, nil, errs.IdRequired
	}
	run, tasks, attempts, err := u.svc.GetDetails(ctx, id)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("svc.GetDetails: %w", err)
	}
	return run, tasks, attempts, nil
}

// Trigger — ручной запуск рана дага.
func (u *Usecase) Trigger(ctx context.Context, dagName string) (string, error) {
	if dagName == "" {
		return "", errs.IdRequired
	}

	dag, _, err := u.dagSvc.Get(ctx, dagName, true)
	if err != nil {
		return "", fmt.Errorf("dagSvc.Get: %w", err)
	}

	runId, err := u.svc.Trigger(ctx, dag, model.TriggerManual)
	if err != nil {
		return "", fmt.Errorf("svc.Trigger: %w", err)
	}

	u.scheduler.Nudge()
	return runId, nil
}
