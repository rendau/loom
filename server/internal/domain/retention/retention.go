// Package retention — TTL завершённых ранов: периодическая очистка
// артефактов и логов (artifact-сервер) и записей БД. Порядок удаления —
// данные раньше записи рана: упавшая на артефактах очистка оставляет ран
// в БД и повторится следующим проходом.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// sweepLimit — ранов за один проход: ограничивает длительность прохода,
// хвост доберут следующие тики.
const sweepLimit = 100

type RunServiceI interface {
	ListExpired(ctx context.Context, before time.Time, limit int64) ([]string, error)
	DeleteRun(ctx context.Context, runId string) error
}

type ArtifactI interface {
	DeleteRunArtifacts(ctx context.Context, runId string) error
}

type TaskLogI interface {
	DeleteRunTaskLogs(ctx context.Context, runId string) error
}

type Service struct {
	runSvc   RunServiceI
	artifact ArtifactI
	tasklog  TaskLogI
	ttl      time.Duration
	tick     time.Duration

	ctx       context.Context
	ctxCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(runSvc RunServiceI, artifact ArtifactI, tasklog TaskLogI, ttl, tick time.Duration) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		runSvc:    runSvc,
		artifact:  artifact,
		tasklog:   tasklog,
		ttl:       ttl,
		tick:      tick,
		ctx:       ctx,
		ctxCancel: cancel,
	}
}

// Start запускает цикл очистки; ttl <= 0 — retention выключен.
func (s *Service) Start() {
	if s.ttl <= 0 {
		slog.Info("run retention disabled")
		return
	}

	slog.Info("run retention started", "ttl", s.ttl, "tick", s.tick)
	s.wg.Go(s.loop)
}

func (s *Service) Stop() {
	s.ctxCancel()
	s.wg.Wait()
}

func (s *Service) loop() {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}

		if _, err := s.Sweep(s.ctx); err != nil && s.ctx.Err() == nil {
			slog.Error("retention sweep", "error", err)
		}
	}
}

// Sweep — один проход очистки: удаляет до sweepLimit просроченных ранов,
// возвращает число удалённых. Ошибка по одному рану не прерывает проход.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	ids, err := s.runSvc.ListExpired(ctx, time.Now().Add(-s.ttl), sweepLimit)
	if err != nil {
		return 0, fmt.Errorf("runSvc.ListExpired: %w", err)
	}

	deleted := 0
	for _, id := range ids {
		if err = s.deleteRun(ctx, id); err != nil {
			slog.Error("retention delete run", "run_id", id, "error", err)
			continue
		}
		deleted++
		slog.Info("run deleted by retention", "run_id", id)
	}

	return deleted, nil
}

func (s *Service) deleteRun(ctx context.Context, runId string) error {
	if err := s.artifact.DeleteRunArtifacts(ctx, runId); err != nil {
		return fmt.Errorf("artifact.DeleteRunArtifacts: %w", err)
	}
	if err := s.tasklog.DeleteRunTaskLogs(ctx, runId); err != nil {
		return fmt.Errorf("tasklog.DeleteRunTaskLogs: %w", err)
	}
	if err := s.runSvc.DeleteRun(ctx, runId); err != nil {
		return fmt.Errorf("runSvc.DeleteRun: %w", err)
	}
	return nil
}
