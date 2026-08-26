package service

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"

	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	"github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/errs"
	"github.com/rendau/loom/server/internal/util"
)

type Service struct {
	repoDb RepoDbI
}

func New(repoDb RepoDbI) *Service {
	return &Service{repoDb: repoDb}
}

func (s *Service) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	items, tCount, err := s.repoDb.List(ctx, pars)
	if err != nil {
		return nil, 0, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, tCount, nil
}

func (s *Service) Get(ctx context.Context, ref model.Ref, errNE bool) (*model.Main, bool, error) {
	result, found, err := s.repoDb.Get(ctx, ref)
	if err != nil {
		return nil, false, fmt.Errorf("repoDb.Get: %w", err)
	}
	if !found {
		if errNE {
			return nil, false, errs.DagNotFound
		}
		return nil, false, nil
	}
	return result, found, nil
}

// Create заводит даг-инстанс от шаблона образа. Существование проекта и
// шаблона проверяет usecase (у него есть доступ к домену project), здесь —
// имя и уникальность.
func (s *Service) Create(ctx context.Context, ref model.Ref, template string) (*model.Main, error) {
	if !manifest.ValidName(ref.Name) {
		return nil, errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("недопустимое имя дага %q", ref.Name)}
	}

	_, found, err := s.Get(ctx, ref, false)
	if err != nil {
		return nil, err
	}
	if found {
		return nil, errs.ErrFull{Err: errs.DagExists,
			Desc: fmt.Sprintf("даг %q в проекте %q уже есть", ref.Name, ref.Project)}
	}

	if err = s.repoDb.Create(ctx, ref, &model.Edit{Template: &template}); err != nil {
		return nil, fmt.Errorf("repoDb.Create: %w", err)
	}

	result, _, err := s.Get(ctx, ref, true)
	return result, err
}

// SetSchedule задаёт cron-расписание и catchup дага; пустое расписание
// снимает его. next_run_at пересчитывается от «сейчас» только при смене
// самого расписания — переключение одного catchup очередь тиков не сбивает.
func (s *Service) SetSchedule(ctx context.Context, ref model.Ref, schedule string, catchup bool) error {
	dag, _, err := s.Get(ctx, ref, true)
	if err != nil {
		return err
	}

	if schedule != "" {
		if _, err = util.CronNext(schedule, time.Now()); err != nil {
			return errs.ErrFull{Err: errs.InvalidRequest,
				Desc: fmt.Sprintf("некорректное расписание %q: %v", schedule, err)}
		}
	}

	err = s.repoDb.Update(ctx, ref, &model.Edit{
		Schedule:   &schedule,
		Catchup:    &catchup,
		ModifiedAt: new(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("repoDb.Update: %w", err)
	}

	if dag.Schedule != schedule {
		if err = s.resetNextRun(ctx, ref, schedule); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) SetPaused(ctx context.Context, ref model.Ref, paused bool) error {
	dag, _, err := s.Get(ctx, ref, true)
	if err != nil {
		return err
	}

	err = s.repoDb.Update(ctx, ref, &model.Edit{
		Paused:     &paused,
		ModifiedAt: new(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("repoDb.Update: %w", err)
	}

	// снятие с паузы: расписание продолжается со следующего срабатывания,
	// пропущенные за время паузы запуски не навёрстываются — кроме
	// catchup-дага: его next_run_at не трогаем, тики наверстает cron-цикл
	if !paused && !(dag.Catchup && !dag.NextRunAt.IsZero()) {
		if err = s.resetNextRun(ctx, ref, dag.Schedule); err != nil {
			return err
		}
	}
	return nil
}

// SetPool задаёт пул слотов дага: он действует на все его таски (в коде
// дага пула нет). Пустая строка снимает пул — таски уедут в default.
// Существование пула проверяет usecase (PoolCheckerI). Пул резолвится при
// триггере рана, поэтому смена действует со следующего рана: у уже
// созданных task instance'ов пул денормализован.
func (s *Service) SetPool(ctx context.Context, ref model.Ref, pool string) error {
	if _, _, err := s.Get(ctx, ref, true); err != nil {
		return err
	}
	if pool != "" && !manifest.ValidName(pool) {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("недопустимое имя пула %q", pool)}
	}

	err := s.repoDb.Update(ctx, ref, &model.Edit{
		Pool:       &pool,
		ModifiedAt: new(time.Now()),
	})
	if err != nil {
		return fmt.Errorf("repoDb.Update: %w", err)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, ref model.Ref) error {
	if _, _, err := s.Get(ctx, ref, true); err != nil {
		return err
	}

	if err := s.repoDb.Delete(ctx, ref); err != nil {
		return fmt.Errorf("repoDb.Delete: %w", err)
	}
	return nil
}

// ListByProject — даги проекта (карточка проекта, каскадные проверки).
func (s *Service) ListByProject(ctx context.Context, project string) ([]*model.Main, error) {
	items, _, err := s.repoDb.List(ctx, &model.ListReq{Project: &project})
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, nil
}

// ── task_resources: оверрайды ресурсов тасков из админки ─────────────────

// ListTaskResources — оверрайды ресурсов тасков дага.
func (s *Service) ListTaskResources(ctx context.Context, ref model.Ref) ([]*model.TaskResourcesEntry, error) {
	if _, _, err := s.Get(ctx, ref, true); err != nil {
		return nil, err
	}
	result, err := s.repoDb.ListTaskResources(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListTaskResources: %w", err)
	}
	return result, nil
}

// SetTaskResources задаёт оверрайд ресурсов таска: значения манифеста —
// рекомендуемые, непустое поле оверрайда приоритетнее при launch. Таск
// должен существовать в текущем манифесте дага; все поля пустые —
// оверрайд удаляется.
func (s *Service) SetTaskResources(ctx context.Context, ref model.Ref, task string, res model.TaskResources) error {
	dag, _, err := s.Get(ctx, ref, true)
	if err != nil {
		return err
	}
	if !lo.ContainsBy(dag.Tasks, func(t model.Task) bool { return t.Name == task }) {
		return errs.ErrFull{Err: errs.TaskNotFound,
			Desc: fmt.Sprintf("таска %q нет в манифесте дага %q", task, ref)}
	}

	for _, q := range []struct{ name, value string }{
		{"cpu_request", res.CPURequest},
		{"cpu_limit", res.CPULimit},
		{"memory_request", res.MemoryRequest},
		{"memory_limit", res.MemoryLimit},
	} {
		if q.value == "" {
			continue
		}
		if _, err = k8sResource.ParseQuantity(q.value); err != nil {
			return errs.ErrFull{Err: errs.InvalidRequest,
				Desc: fmt.Sprintf("некорректное значение %s=%q", q.name, q.value)}
		}
	}

	if res == (model.TaskResources{}) {
		if _, err = s.repoDb.DeleteTaskResources(ctx, ref, task); err != nil {
			return fmt.Errorf("repoDb.DeleteTaskResources: %w", err)
		}
		return nil
	}

	if err = s.repoDb.SetTaskResources(ctx, ref, task, res); err != nil {
		return fmt.Errorf("repoDb.SetTaskResources: %w", err)
	}
	return nil
}

// DeleteTaskResources снимает оверрайд таска (возврат к манифесту).
func (s *Service) DeleteTaskResources(ctx context.Context, ref model.Ref, task string) error {
	if _, _, err := s.Get(ctx, ref, true); err != nil {
		return err
	}
	found, err := s.repoDb.DeleteTaskResources(ctx, ref, task)
	if err != nil {
		return fmt.Errorf("repoDb.DeleteTaskResources: %w", err)
	}
	if !found {
		return errs.ObjectNotFound
	}
	return nil
}

// GetTaskResources — оверрайд одного таска для launch; nil — оверрайда нет.
func (s *Service) GetTaskResources(ctx context.Context, ref model.Ref, task string) (*model.TaskResources, error) {
	res, err := s.repoDb.GetTaskResources(ctx, ref, task)
	if err != nil {
		return nil, fmt.Errorf("repoDb.GetTaskResources: %w", err)
	}
	return res, nil
}

// ListLastRuns — последние perDag ранов каждого дага (статус-стрип админки).
func (s *Service) ListLastRuns(ctx context.Context, refs []model.Ref, perDag int) (map[model.Ref][]model.LastRun, error) {
	if len(refs) == 0 {
		return map[model.Ref][]model.LastRun{}, nil
	}
	result, err := s.repoDb.ListLastRuns(ctx, refs, perDag)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListLastRuns: %w", err)
	}
	return result, nil
}

// resetNextRun пересчитывает next_run_at дага от текущего момента; пустое
// расписание сбрасывает его в null.
func (s *Service) resetNextRun(ctx context.Context, ref model.Ref, schedule string) error {
	var next *time.Time
	if schedule != "" {
		t, err := util.CronNext(schedule, time.Now())
		if err != nil {
			return errs.ErrFull{Err: errs.InvalidManifest, Desc: err.Error()}
		}
		next = &t
	}

	if err := s.repoDb.SetNextRun(ctx, ref, next); err != nil {
		return fmt.Errorf("repoDb.SetNextRun: %w", err)
	}
	return nil
}

// ── операции cron-планировщика ──────────────────────────

// ListDueSchedules возвращает даги с расписанием, чей next_run_at наступил
// или не инициализирован (null при непустом schedule).
func (s *Service) ListDueSchedules(ctx context.Context) ([]*model.Main, error) {
	refs, err := s.repoDb.ListDueRefs(ctx)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListDueRefs: %w", err)
	}

	result := make([]*model.Main, 0, len(refs))
	for _, ref := range refs {
		dag, found, err := s.Get(ctx, ref, false)
		if err != nil {
			return nil, err
		}
		if found {
			result = append(result, dag)
		}
	}
	return result, nil
}

// AdvanceNextRun двигает next_run_at дага вперёд compare-and-swap'ом:
// false — значение уже сдвинул кто-то другой (гонка инстансов), триггерить
// этот тик не нужно.
func (s *Service) AdvanceNextRun(ctx context.Context, ref model.Ref, from, to time.Time) (bool, error) {
	ok, err := s.repoDb.AdvanceNextRun(ctx, ref, from, to)
	if err != nil {
		return false, fmt.Errorf("repoDb.AdvanceNextRun: %w", err)
	}
	return ok, nil
}
