package run

import (
	"context"
	"fmt"
	"time"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/run/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

type Usecase struct {
	svc       ServiceI
	dagSvc    DagServiceI
	scheduler SchedulerI
	authz     AuthzI
}

func New(svc ServiceI, dagSvc DagServiceI, scheduler SchedulerI, authz AuthzI) *Usecase {
	return &Usecase{svc: svc, dagSvc: dagSvc, scheduler: scheduler, authz: authz}
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

func (u *Usecase) Get(ctx context.Context, id string) (*model.Main, []dagModel.Task, []*model.TaskInstance, []*model.Attempt, error) {
	if id == "" {
		return nil, nil, nil, nil, errs.IdRequired
	}
	run, manifestTasks, tasks, attempts, err := u.svc.GetDetails(ctx, id)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("svc.GetDetails: %w", err)
	}
	return run, manifestTasks, tasks, attempts, nil
}

// Trigger — ручной запуск рана дага; params — опциональный JSON-объект
// параметров (nil — без параметров).
func (u *Usecase) Trigger(ctx context.Context, dagName string, params []byte) (string, error) {
	if dagName == "" {
		return "", errs.IdRequired
	}
	if err := u.authz.RequireDag(ctx, dagName); err != nil {
		return "", err
	}

	dag, _, err := u.dagSvc.Get(ctx, dagName, true)
	if err != nil {
		return "", fmt.Errorf("dagSvc.Get: %w", err)
	}

	runId, err := u.svc.Trigger(ctx, dag, model.TriggerSpec{Trigger: model.TriggerManual, Params: params})
	if err != nil {
		return "", fmt.Errorf("svc.Trigger: %w", err)
	}

	u.scheduler.Nudge()
	return runId, nil
}

// maxBackfillRuns — лимит ранов одного вызова Backfill: защита от опечатки
// в периоде (from=2020 превратился бы в тысячи ранов).
const maxBackfillRuns = 100

// Backfill создаёт раны за период [from, to): по рану на каждый тик
// cron-расписания дага, trigger=backfill, logical_date=тик. Идемпотентности
// нет: повторный вызов за тот же период создаст дубли ранов.
func (u *Usecase) Backfill(ctx context.Context, dagName string, from, to time.Time, params []byte) ([]string, error) {
	if dagName == "" {
		return nil, errs.IdRequired
	}
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: "требуется период from < to"}
	}
	if err := u.authz.RequireDag(ctx, dagName); err != nil {
		return nil, err
	}

	dag, _, err := u.dagSvc.Get(ctx, dagName, true)
	if err != nil {
		return nil, fmt.Errorf("dagSvc.Get: %w", err)
	}
	if dag.Schedule == "" {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: "backfill доступен только дагу с расписанием"}
	}

	// тики расписания в [from, to): CronNext строго после аргумента, поэтому
	// сам from включаем сдвигом на секунду назад
	var ticks []time.Time
	for t := from.Add(-time.Second); ; {
		t, err = util.CronNext(dag.Schedule, t)
		if err != nil {
			return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: err.Error()}
		}
		if !t.Before(to) {
			break
		}
		ticks = append(ticks, t)
		if len(ticks) > maxBackfillRuns {
			return nil, errs.ErrFull{Err: errs.InvalidRequest,
				Desc: fmt.Sprintf("в периоде больше %d тиков расписания — сузьте период", maxBackfillRuns)}
		}
	}
	if len(ticks) == 0 {
		return nil, errs.ErrFull{Err: errs.InvalidRequest, Desc: "в периоде нет тиков расписания"}
	}

	runIds := make([]string, 0, len(ticks))
	for _, tick := range ticks {
		runId, err := u.svc.Trigger(ctx, dag, model.TriggerSpec{
			Trigger:     model.TriggerBackfill,
			Params:      params,
			LogicalDate: tick,
		})
		if err != nil {
			return runIds, fmt.Errorf("svc.Trigger (tick %s, создано %d ранов): %w", tick, len(runIds), err)
		}
		runIds = append(runIds, runId)
	}

	u.scheduler.Nudge()
	return runIds, nil
}

// ── значения тасков ─────────────────────────────────────

func (u *Usecase) PushValue(ctx context.Context, ref model.AttemptRef, key string, value []byte) error {
	if ref.RunId == "" || ref.Task == "" || key == "" {
		return errs.IdRequired
	}
	if err := u.svc.PushValue(ctx, ref, key, value); err != nil {
		return fmt.Errorf("svc.PushValue: %w", err)
	}
	return nil
}

func (u *Usecase) PullValue(ctx context.Context, runId, task, key string) (*model.TaskValue, error) {
	if runId == "" || task == "" || key == "" {
		return nil, errs.IdRequired
	}
	v, err := u.svc.PullValue(ctx, runId, task, key)
	if err != nil {
		return nil, fmt.Errorf("svc.PullValue: %w", err)
	}
	return v, nil
}

func (u *Usecase) ListValues(ctx context.Context, runId string) ([]*model.TaskValue, error) {
	if runId == "" {
		return nil, errs.IdRequired
	}
	items, err := u.svc.ListValues(ctx, runId)
	if err != nil {
		return nil, fmt.Errorf("svc.ListValues: %w", err)
	}
	return items, nil
}

// RetryTask — ретрай таска и его downstream-подграфа на завершённом ране.
func (u *Usecase) RetryTask(ctx context.Context, runId, task string) error {
	if runId == "" || task == "" {
		return errs.IdRequired
	}

	run, _, err := u.svc.Get(ctx, runId, true)
	if err != nil {
		return fmt.Errorf("svc.Get: %w", err)
	}
	if err = u.authz.RequireDag(ctx, run.DagName); err != nil {
		return err
	}

	if err := u.svc.RetryTask(ctx, runId, task); err != nil {
		return fmt.Errorf("svc.RetryTask: %w", err)
	}

	u.scheduler.Nudge()
	return nil
}
