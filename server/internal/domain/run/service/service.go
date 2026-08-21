package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	json "github.com/goccy/go-json"
	"github.com/samber/lo"

	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	poolModel "github.com/rendau/loom/server/internal/domain/pool/model"
	"github.com/rendau/loom/server/internal/domain/run/model"
	"github.com/rendau/loom/server/internal/errs"
)

type Service struct {
	repoDb RepoDbI
	txm    TxManagerI
}

func New(repoDb RepoDbI, txm TxManagerI) *Service {
	return &Service{repoDb: repoDb, txm: txm}
}

func (s *Service) List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error) {
	items, tCount, err := s.repoDb.ListRuns(ctx, pars)
	if err != nil {
		return nil, 0, fmt.Errorf("repoDb.ListRuns: %w", err)
	}
	return items, tCount, nil
}

func (s *Service) Get(ctx context.Context, id string, errNE bool) (*model.Main, bool, error) {
	result, found, err := s.repoDb.GetRun(ctx, id)
	if err != nil {
		return nil, false, fmt.Errorf("repoDb.GetRun: %w", err)
	}
	if !found {
		if errNE {
			return nil, false, errs.RunNotFound
		}
		return nil, false, nil
	}
	return result, found, nil
}

// GetDetails возвращает ран вместе с task instance'ами, попытками и тасками
// из снапшота манифеста (структура графа на момент триггера).
func (s *Service) GetDetails(ctx context.Context, id string) (*model.Main, []dagModel.Task, []*model.TaskInstance, []*model.Attempt, error) {
	run, _, err := s.Get(ctx, id, true)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	m, err := manifest.Parse(run.Manifest)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("manifest.Parse: %w", err)
	}

	tasks, err := s.repoDb.ListTaskInstances(ctx, id)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("repoDb.ListTaskInstances: %w", err)
	}

	attempts, err := s.repoDb.ListAttempts(ctx, id)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("repoDb.ListAttempts: %w", err)
	}

	return run, m.Tasks, tasks, attempts, nil
}

// Trigger создаёт ран дага: снапшот манифеста и пиннинг к digest на момент
// триггера, все таски — pending; дальше их раскручивает планировщик.
func (s *Service) Trigger(ctx context.Context, dag *dagModel.Main, spec model.TriggerSpec) (string, error) {
	if len(dag.Tasks) == 0 {
		return "", errs.ErrFull{Err: errs.InvalidManifest, Desc: "у дага нет тасков"}
	}
	if len(spec.Params) > model.MaxParamsSize {
		return "", errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("params больше лимита %d байт", model.MaxParamsSize)}
	}
	if spec.LogicalDate.IsZero() {
		spec.LogicalDate = time.Now()
	}

	run := &model.Main{
		Id:          newRunId(dag.Name),
		DagName:     dag.Name,
		Image:       dag.Image,
		ImageDigest: dag.ImageDigest,
		Trigger:     spec.Trigger,
		Status:      model.RunStatusRunning,
		Manifest:    dag.Manifest,
		Params:      spec.Params,
		LogicalDate: spec.LogicalDate,
	}

	err := s.txm.TxFn(ctx, func(ctx context.Context) error {
		if err := s.repoDb.CreateRun(ctx, run); err != nil {
			return fmt.Errorf("repoDb.CreateRun: %w", err)
		}

		tasks := lo.Map(dag.Tasks, func(t dagModel.Task, _ int) model.TaskSeed {
			return model.TaskSeed{
				Task:     t.Name,
				Pool:     lo.CoalesceOrEmpty(t.Pool, poolModel.DefaultPool),
				Priority: int32(t.Priority),
			}
		})
		if err := s.repoDb.CreateTaskInstances(ctx, run.Id, tasks); err != nil {
			return fmt.Errorf("repoDb.CreateTaskInstances: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	return run.Id, nil
}

// RetryTask возвращает таск завершённого рана в очередь и сбрасывает его
// downstream-подграф (транзитивно) в pending; ран реактивируется (running),
// дальше граф раскручивает планировщик. Разрешён только на завершённом
// ране: планировщик не трогает finished-раны, поэтому сброс статусов не
// гонится с раскруткой графа. Новая попытка получает attempt+1 обычным
// claim'ом, старые попытки остаются историей.
func (s *Service) RetryTask(ctx context.Context, runId, task string) error {
	run, _, err := s.Get(ctx, runId, true)
	if err != nil {
		return err
	}
	if run.Status == model.RunStatusRunning {
		return errs.ErrFull{Err: errs.RunNotFinished, Desc: "ретрай доступен только на завершённом ране"}
	}

	m, err := manifest.Parse(run.Manifest)
	if err != nil {
		return fmt.Errorf("manifest.Parse: %w", err)
	}
	if !lo.ContainsBy(m.Tasks, func(t dagModel.Task) bool { return t.Name == task }) {
		return errs.ErrFull{Err: errs.TaskNotFound, Desc: fmt.Sprintf("таска %q нет в манифесте рана", task)}
	}

	tis, err := s.repoDb.ListTaskInstances(ctx, runId)
	if err != nil {
		return fmt.Errorf("repoDb.ListTaskInstances: %w", err)
	}
	ti, ok := lo.Find(tis, func(t *model.TaskInstance) bool { return t.Task == task })
	if !ok {
		return errs.TaskNotFound
	}
	if ti.Status != model.TaskStatusSuccess && ti.Status != model.TaskStatusFailed {
		// на завершённом ране остаётся только upstream_failed: сам таск не
		// исполнялся — ретраить нужно его упавшую зависимость
		return errs.ErrFull{Err: errs.TaskNotRetryable, Desc: "таск не исполнялся: ретрайте упавшую зависимость"}
	}

	downstream := downstreamOf(m.Tasks, task)

	err = s.txm.TxFn(ctx, func(ctx context.Context) error {
		applied, txErr := s.repoDb.RetryTaskSubgraph(ctx, runId, task, downstream)
		if txErr != nil {
			return fmt.Errorf("repoDb.RetryTaskSubgraph: %w", txErr)
		}
		if !applied {
			// гонка с конкурентным ретраем — состояние ушло из-под проверок выше
			return errs.ErrFull{Err: errs.TaskNotRetryable, Desc: "состояние рана изменилось, повторите запрос"}
		}
		return nil
	})
	return err
}

// downstreamOf возвращает транзитивных потомков таска по рёбрам манифеста
// (сам таск не входит).
func downstreamOf(tasks []dagModel.Task, root string) []string {
	children := map[string][]string{}
	for _, t := range tasks {
		for _, d := range t.DependsOn {
			children[d.Task] = append(children[d.Task], t.Name)
		}
	}

	var result []string
	seen := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range children[cur] {
			if !seen[c] {
				seen[c] = true
				result = append(result, c)
				queue = append(queue, c)
			}
		}
	}
	return result
}

// ── значения тасков (решение №25) ───────────────────────

// valueKeyRe — допустимые ключи значений; согласовано с ограничениями имён
// артефактов SDK.
var valueKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// PushValue сохраняет значение таска (upsert по ключу). Автор — попытка ref;
// пуш от неактуальной попытки отклоняется: зомби прошлой попытки не должен
// перезаписать значение свежего ретрая.
func (s *Service) PushValue(ctx context.Context, ref model.AttemptRef, key string, value []byte) error {
	if !valueKeyRe.MatchString(key) {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: fmt.Sprintf("недопустимый ключ значения %q", key)}
	}
	if len(value) == 0 {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "пустое значение"}
	}
	if len(value) > model.MaxValueSize {
		return errs.ErrFull{Err: errs.InvalidRequest,
			Desc: fmt.Sprintf("значение больше лимита %d байт", model.MaxValueSize)}
	}
	if !json.Valid(value) {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "значение — не валидный JSON"}
	}

	tis, err := s.repoDb.ListTaskInstances(ctx, ref.RunId)
	if err != nil {
		return fmt.Errorf("repoDb.ListTaskInstances: %w", err)
	}
	ti, ok := lo.Find(tis, func(t *model.TaskInstance) bool { return t.Task == ref.Task })
	if !ok {
		return errs.TaskNotFound
	}
	if ti.Attempt != ref.Attempt {
		return errs.ErrFull{Err: errs.AttemptOutdated,
			Desc: fmt.Sprintf("текущая попытка таска — %d", ti.Attempt)}
	}

	err = s.repoDb.UpsertTaskValue(ctx, &model.TaskValue{
		RunId: ref.RunId,
		Task:  ref.Task,
		Key:   key,
		Value: value,
	})
	if err != nil {
		return fmt.Errorf("repoDb.UpsertTaskValue: %w", err)
	}
	return nil
}

func (s *Service) PullValue(ctx context.Context, runId, task, key string) (*model.TaskValue, error) {
	v, found, err := s.repoDb.GetTaskValue(ctx, runId, task, key)
	if err != nil {
		return nil, fmt.Errorf("repoDb.GetTaskValue: %w", err)
	}
	if !found {
		return nil, errs.ValueNotFound
	}
	return v, nil
}

func (s *Service) ListValues(ctx context.Context, runId string) ([]*model.TaskValue, error) {
	if _, _, err := s.Get(ctx, runId, true); err != nil {
		return nil, err
	}

	items, err := s.repoDb.ListTaskValues(ctx, runId)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListTaskValues: %w", err)
	}
	return items, nil
}

// ── операции планировщика ───────────────────────────────

// ListActive возвращает незавершённые раны.
func (s *Service) ListActive(ctx context.Context) ([]*model.Main, error) {
	items, _, err := s.repoDb.ListRuns(ctx, &model.ListReq{Status: new(model.RunStatusRunning)})
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListRuns: %w", err)
	}
	return items, nil
}

func (s *Service) ListTaskInstances(ctx context.Context, runId string) ([]*model.TaskInstance, error) {
	items, err := s.repoDb.ListTaskInstances(ctx, runId)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListTaskInstances: %w", err)
	}
	return items, nil
}

func (s *Service) PromoteTasks(ctx context.Context, runId string, tasks []string, fromStatus, toStatus string) error {
	if err := s.repoDb.PromoteTaskInstances(ctx, runId, tasks, fromStatus, toStatus); err != nil {
		return fmt.Errorf("repoDb.PromoteTaskInstances: %w", err)
	}
	return nil
}

// ClaimQueued забирает queued-таски из очереди и заводит каждому попытку
// (starting) — атомарно, одной транзакцией.
func (s *Service) ClaimQueued(ctx context.Context, limit int64) ([]model.ClaimedTask, error) {
	var claimed []model.ClaimedTask

	err := s.txm.TxFn(ctx, func(ctx context.Context) error {
		var err error
		claimed, err = s.repoDb.ClaimQueuedTasks(ctx, limit)
		if err != nil {
			return fmt.Errorf("repoDb.ClaimQueuedTasks: %w", err)
		}

		for _, c := range claimed {
			ref := model.AttemptRef{RunId: c.RunId, Task: c.Task, Attempt: c.Attempt}
			if err = s.repoDb.CreateAttempt(ctx, ref); err != nil {
				return fmt.Errorf("repoDb.CreateAttempt: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

func (s *Service) GetAttempt(ctx context.Context, ref model.AttemptRef, errNE bool) (*model.Attempt, bool, error) {
	result, found, err := s.repoDb.GetAttempt(ctx, ref)
	if err != nil {
		return nil, false, fmt.Errorf("repoDb.GetAttempt: %w", err)
	}
	if !found {
		if errNE {
			return nil, false, errs.AttemptNotFound
		}
		return nil, false, nil
	}
	return result, found, nil
}

func (s *Service) MarkAttemptRunning(ctx context.Context, ref model.AttemptRef) (bool, error) {
	var applied bool
	err := s.txm.TxFn(ctx, func(ctx context.Context) error {
		var err error
		applied, err = s.repoDb.MarkAttemptRunning(ctx, ref)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("repoDb.MarkAttemptRunning: %w", err)
	}
	return applied, nil
}

// FinalizeAttempt фиксирует завершение попытки; false — попытка уже была
// терминальна (дубль события или страховочный вызов). retryAt задаёт
// отложенный ретрай неуспешной попытки (up_for_retry вместо failed).
func (s *Service) FinalizeAttempt(ctx context.Context, ref model.AttemptRef, exit model.ExitInfo, retryAt *time.Time) (bool, error) {
	var applied bool
	err := s.txm.TxFn(ctx, func(ctx context.Context) error {
		var err error
		applied, err = s.repoDb.FinalizeAttempt(ctx, ref, exit, retryAt)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("repoDb.FinalizeAttempt: %w", err)
	}
	return applied, nil
}

// PromoteRetries возвращает в очередь up_for_retry-таски с истёкшим backoff.
func (s *Service) PromoteRetries(ctx context.Context) (int64, error) {
	n, err := s.repoDb.PromoteRetries(ctx)
	if err != nil {
		return 0, fmt.Errorf("repoDb.PromoteRetries: %w", err)
	}
	return n, nil
}

// ListStaleAttempts возвращает незавершённые попытки старше olderThan —
// кандидатов на зомби-детект.
func (s *Service) ListStaleAttempts(ctx context.Context, olderThan time.Time) ([]model.StaleAttempt, error) {
	items, err := s.repoDb.ListStaleAttempts(ctx, olderThan)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListStaleAttempts: %w", err)
	}
	return items, nil
}

// ── retention ───────────────────────────────────────────

// ListExpired возвращает id завершённых ранов с истёкшим TTL.
func (s *Service) ListExpired(ctx context.Context, before time.Time, limit int64) ([]string, error) {
	ids, err := s.repoDb.ListExpiredRuns(ctx, before, limit)
	if err != nil {
		return nil, fmt.Errorf("repoDb.ListExpiredRuns: %w", err)
	}
	return ids, nil
}

// DeleteRun удаляет ран со всеми тасками и попытками.
func (s *Service) DeleteRun(ctx context.Context, runId string) error {
	if err := s.repoDb.DeleteRun(ctx, runId); err != nil {
		return fmt.Errorf("repoDb.DeleteRun: %w", err)
	}
	return nil
}

func (s *Service) FinishRun(ctx context.Context, runId, status string) error {
	if err := s.repoDb.FinishRun(ctx, runId, status); err != nil {
		return fmt.Errorf("repoDb.FinishRun: %w", err)
	}
	return nil
}

// newRunId — читаемый уникальный id рана: <даг>-<utc-таймстамп>-<случайный
// суффикс>. Формат совместим с ограничениями artifact-сервера на run_id.
func newRunId(dagName string) string {
	suffix := make([]byte, 2)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("%s-%s-%s", dagName, time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(suffix))
}
