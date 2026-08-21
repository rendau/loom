package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/samber/lo"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
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

// GetDetails возвращает ран вместе с task instance'ами и попытками.
func (s *Service) GetDetails(ctx context.Context, id string) (*model.Main, []*model.TaskInstance, []*model.Attempt, error) {
	run, _, err := s.Get(ctx, id, true)
	if err != nil {
		return nil, nil, nil, err
	}

	tasks, err := s.repoDb.ListTaskInstances(ctx, id)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("repoDb.ListTaskInstances: %w", err)
	}

	attempts, err := s.repoDb.ListAttempts(ctx, id)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("repoDb.ListAttempts: %w", err)
	}

	return run, tasks, attempts, nil
}

// Trigger создаёт ран дага: снапшот манифеста и пиннинг к digest на момент
// триггера, все таски — pending; дальше их раскручивает планировщик.
func (s *Service) Trigger(ctx context.Context, dag *dagModel.Main, trigger string) (string, error) {
	if len(dag.Tasks) == 0 {
		return "", errs.ErrFull{Err: errs.InvalidManifest, Desc: "у дага нет тасков"}
	}

	run := &model.Main{
		Id:          newRunId(dag.Name),
		DagName:     dag.Name,
		Image:       dag.Image,
		ImageDigest: dag.ImageDigest,
		Trigger:     trigger,
		Status:      model.RunStatusRunning,
		Manifest:    dag.Manifest,
	}

	err := s.txm.TxFn(ctx, func(ctx context.Context) error {
		if err := s.repoDb.CreateRun(ctx, run); err != nil {
			return fmt.Errorf("repoDb.CreateRun: %w", err)
		}

		tasks := lo.Map(dag.Tasks, func(t dagModel.Task, _ int) string { return t.Name })
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
