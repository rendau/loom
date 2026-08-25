// Package retention — очистка завершённых ранов: периодическое удаление
// артефактов и логов (artifact-сервер) и записей БД. Лимиты — настройки
// из БД (run_ttl и run_keep_last, скоуп дага перекрывает глобальный); ран
// удаляется, если нарушает любой из них. Порядок удаления — данные раньше
// записи рана: упавшая на артефактах очистка оставляет ран в БД и
// повторится следующим проходом.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	settingModel "github.com/rendau/loom/server/internal/domain/setting/model"
)

// sweepLimit — ранов за один проход: ограничивает длительность прохода,
// хвост доберут следующие тики.
const sweepLimit = 100

type RunServiceI interface {
	ListRetentionDags(ctx context.Context) ([]string, error)
	ListExpired(ctx context.Context, dagName string, before *time.Time, keepLast, limit int64) ([]string, error)
	DeleteRun(ctx context.Context, runId string) error
}

// SettingsI — резолв retention-лимитов по скоупам дагов.
type SettingsI interface {
	ResolveForDags(ctx context.Context, dagNames []string) (map[string]settingModel.Effective, error)
}

type ArtifactI interface {
	DeleteRunArtifacts(ctx context.Context, runId string) error
}

type TaskLogI interface {
	DeleteRunTaskLogs(ctx context.Context, runId string) error
}

// SessionCleanerI — чистка истёкших сессий админки (тем же циклом, что и
// TTL ранов: обе операции — фоновая уборка).
type SessionCleanerI interface {
	CleanupSessions(ctx context.Context) (int64, error)
}

type Service struct {
	runSvc   RunServiceI
	artifact ArtifactI
	tasklog  TaskLogI
	sessions SessionCleanerI
	settings SettingsI
	tick     time.Duration

	ctx       context.Context
	ctxCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(runSvc RunServiceI, artifact ArtifactI, tasklog TaskLogI, sessions SessionCleanerI,
	settings SettingsI, tick time.Duration,
) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		runSvc:    runSvc,
		artifact:  artifact,
		tasklog:   tasklog,
		sessions:  sessions,
		settings:  settings,
		tick:      tick,
		ctx:       ctx,
		ctxCancel: cancel,
	}
}

// Start запускает цикл очистки; лимиты читаются из настроек на каждом
// проходе (правки в админке подхватываются без рестарта).
func (s *Service) Start() {
	slog.Info("run retention started", "tick", s.tick)
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
		if s.sessions != nil {
			if _, err := s.sessions.CleanupSessions(s.ctx); err != nil && s.ctx.Err() == nil {
				slog.Error("sessions cleanup", "error", err)
			}
		}
	}
}

// Sweep — один проход очистки: по каждому дагу с завершёнными ранами
// резолвит эффективные лимиты и удаляет до sweepLimit ранов суммарно,
// возвращает число удалённых. Ошибка по одному рану не прерывает проход.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	dags, err := s.runSvc.ListRetentionDags(ctx)
	if err != nil {
		return 0, fmt.Errorf("runSvc.ListRetentionDags: %w", err)
	}
	if len(dags) == 0 {
		return 0, nil
	}

	effective, err := s.settings.ResolveForDags(ctx, dags)
	if err != nil {
		return 0, fmt.Errorf("settings.ResolveForDags: %w", err)
	}

	deleted := 0
	for _, dag := range dags {
		eff := effective[dag]
		if eff.RunTTL <= 0 && eff.RunKeepLast <= 0 {
			continue
		}
		if deleted >= sweepLimit {
			break
		}

		var before *time.Time
		if eff.RunTTL > 0 {
			before = new(time.Now().Add(-eff.RunTTL))
		}

		ids, err := s.runSvc.ListExpired(ctx, dag, before, eff.RunKeepLast, int64(sweepLimit-deleted))
		if err != nil {
			slog.Error("retention list expired", "dag", dag, "error", err)
			continue
		}

		for _, id := range ids {
			if err = s.deleteRun(ctx, id); err != nil {
				slog.Error("retention delete run", "run_id", id, "error", err)
				continue
			}
			deleted++
			slog.Info("run deleted by retention", "run_id", id)
		}
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
