// Package service (projectreg) — асинхронная очередь регистраций проектов:
// RegisterProject лишь ставит запись в очередь, а pull + describe выполняет
// фоновый воркер (claim через FOR UPDATE SKIP LOCKED — инстансы control
// plane не конфликтуют). Админка поллит статусы записей.
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/rendau/loom/server/internal/domain/projectreg/model"
	"github.com/rendau/loom/server/internal/errs"
)

// maxConcurrent — одновременных обработок на инстанс: pull образа тяжёлый,
// но и сериализовать всё в одну очередь нельзя — ручная регистрация не
// должна ждать пачку auto-обновлений.
const maxConcurrent = 3

// maintenanceEvery — период обслуживания очереди (FailStale + TTL-чистка).
const maintenanceEvery = 5 * time.Minute

type Service struct {
	repoDb     RepoDbI
	settings   SettingsI
	tick       time.Duration
	staleAfter time.Duration

	processor ProcessorI
	sem       chan struct{}
	nudgeCh   chan struct{}

	lastMaintenance time.Time

	ctx       context.Context
	ctxCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(repoDb RepoDbI, settings SettingsI, tick, staleAfter time.Duration) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		repoDb:     repoDb,
		settings:   settings,
		tick:       tick,
		staleAfter: staleAfter,
		sem:        make(chan struct{}, maxConcurrent),
		nudgeCh:    make(chan struct{}, 1),
		ctx:        ctx,
		ctxCancel:  cancel,
	}
}

// Enqueue ставит регистрацию в очередь. Для source=auto действует дедуп:
// незавершённая регистрация того же проекта — новая не создаётся (nil).
func (s *Service) Enqueue(ctx context.Context, spec model.EnqueueSpec) (*model.Main, error) {
	if spec.Source == model.SourceAuto {
		active, err := s.repoDb.HasActive(ctx, spec.ProjectName)
		if err != nil {
			return nil, fmt.Errorf("repoDb.HasActive: %w", err)
		}
		if active {
			return nil, nil
		}
	}

	m := &model.Main{
		Id:          newRegId(),
		ProjectName: spec.ProjectName,
		Image:       spec.Image,
		Source:      spec.Source,
		AutoUpdate:  spec.AutoUpdate,
		CreateDags:  spec.CreateDags,
		Status:      model.StatusPending,
	}
	if err := s.repoDb.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("repoDb.Create: %w", err)
	}

	s.Nudge()
	return m, nil
}

// EnqueueAuto — постановка авто-перерегистрации от dagsync: проект
// известен сразу, его настройки не трогаются. Новые даги образа
// авто-обновление не заводит — это решение админа.
func (s *Service) EnqueueAuto(ctx context.Context, projectName, image string) error {
	_, err := s.Enqueue(ctx, model.EnqueueSpec{
		ProjectName: projectName,
		Image:       image,
		Source:      model.SourceAuto,
	})
	return err
}

func (s *Service) Get(ctx context.Context, id string) (*model.Main, error) {
	result, found, err := s.repoDb.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("repoDb.Get: %w", err)
	}
	if !found {
		return nil, errs.RegistrationNotFound
	}
	return result, nil
}

func (s *Service) List(ctx context.Context, req *model.ListReq) ([]*model.Main, error) {
	items, err := s.repoDb.List(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("repoDb.List: %w", err)
	}
	return items, nil
}

// Nudge будит воркер, не дожидаясь тика (после Enqueue).
func (s *Service) Nudge() {
	select {
	case s.nudgeCh <- struct{}{}:
	default:
	}
}

// Start запускает воркер очереди; processor задаётся здесь, а не в New —
// разрывает цикл зависимостей (обработчик регистраций сам зовёт Enqueue).
func (s *Service) Start(processor ProcessorI) {
	s.processor = processor
	slog.Info("project registration worker started", "tick", s.tick)
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
		case <-s.nudgeCh:
		}

		if err := s.pass(s.ctx); err != nil && s.ctx.Err() == nil {
			slog.Error("project registration pass", "error", err)
		}
	}
}

func (s *Service) pass(ctx context.Context) error {
	if time.Since(s.lastMaintenance) > maintenanceEvery {
		s.lastMaintenance = time.Now()
		s.maintain(ctx)
	}

	free := int64(maxConcurrent - len(s.sem))
	if free <= 0 {
		return nil
	}

	regs, err := s.repoDb.ClaimPending(ctx, free)
	if err != nil {
		return fmt.Errorf("repoDb.ClaimPending: %w", err)
	}

	for _, reg := range regs {
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return nil
		}
		s.wg.Go(func() {
			defer func() { <-s.sem }()
			s.process(reg)
		})
	}
	return nil
}

func (s *Service) process(reg *model.Main) {
	slog.Info("project registration started", "id", reg.Id,
		"project", reg.ProjectName, "image", reg.Image, "source", reg.Source)

	result, err := s.processor.Process(s.ctx, reg)

	// финализация и при погашенном ctx (graceful shutdown): запись не должна
	// зависнуть в running до FailStale
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 10*time.Second)
	defer cancel()

	if err != nil {
		slog.Error("project registration failed", "id", reg.Id,
			"project", reg.ProjectName, "image", reg.Image, "error", err)
		if fErr := s.repoDb.Finish(finishCtx, reg.Id, model.StatusFailed, err.Error(), result); fErr != nil {
			slog.Error("project registration finish", "id", reg.Id, "error", fErr)
		}
		return
	}

	slog.Info("project registered", "id", reg.Id, "project", reg.ProjectName,
		"image", reg.Image, "dags", len(result))
	if fErr := s.repoDb.Finish(finishCtx, reg.Id, model.StatusSuccess, "", result); fErr != nil {
		slog.Error("project registration finish", "id", reg.Id, "error", fErr)
	}
}

// maintain — обслуживание очереди: зависшие после смерти инстанса running →
// failed, завершённые старше TTL удаляются. Ошибки не прерывают проход.
func (s *Service) maintain(ctx context.Context) {
	if n, err := s.repoDb.FailStale(ctx, time.Now().Add(-s.staleAfter)); err != nil {
		slog.Error("project registration fail stale", "error", err)
	} else if n > 0 {
		slog.Warn("stale project registrations failed", "count", n)
	}

	// TTL истории — глобальная настройка dag_reg_ttl из БД (правки в
	// админке подхватываются без рестарта)
	eff, err := s.settings.ResolveGlobal(ctx)
	if err != nil {
		slog.Error("project registration resolve settings", "error", err)
		return
	}
	if eff.DagRegTTL > 0 {
		if _, err = s.repoDb.DeleteFinishedBefore(ctx, time.Now().Add(-eff.DagRegTTL)); err != nil {
			slog.Error("project registration ttl cleanup", "error", err)
		}
	}
}

// newRegId — читаемый уникальный id регистрации.
func newRegId() string {
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("reg-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(suffix))
}
