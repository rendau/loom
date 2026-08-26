// Package dagsync — авто-обновление проектов (аналог keel poll):
// периодический digest-чек тега в registry для проектов с auto_update; при
// изменении перерегистрация ставится в очередь projectreg (источник auto) —
// её статус виден в админке. Один запрос на проект, а не на каждый его даг.
// Сломанный новый образ шаблоны не трогает — регистрация завершится failed
// и повторится следующим digest-чеком.
package dagsync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	projectModel "github.com/rendau/loom/server/internal/domain/project/model"
)

type ProjectServiceI interface {
	ListAutoUpdate(ctx context.Context) ([]*projectModel.Main, error)
}

// DigestResolverI — дешёвый резолв текущего digest'а тега («sha256:...»)
// через registry API, без скачивания образа.
type DigestResolverI interface {
	ResolveDigest(ctx context.Context, image string) (string, error)
}

// EnqueuerI — постановка перерегистрации в очередь projectreg: сам pull +
// describe выполняет её воркер, дедуп повторных постановок — на очереди.
type EnqueuerI interface {
	EnqueueAuto(ctx context.Context, projectName, image string) error
}

type Service struct {
	projects ProjectServiceI
	resolver DigestResolverI
	enqueuer EnqueuerI
	tick     time.Duration

	ctx       context.Context
	ctxCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(projects ProjectServiceI, resolver DigestResolverI, enqueuer EnqueuerI, tick time.Duration) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		projects:  projects,
		resolver:  resolver,
		enqueuer:  enqueuer,
		tick:      tick,
		ctx:       ctx,
		ctxCancel: cancel,
	}
}

// Start запускает цикл синка; tick <= 0 — авто-обновление выключено.
func (s *Service) Start() {
	if s.tick <= 0 {
		slog.Info("project auto-update disabled (DAG_SYNC_TICK=0)")
		return
	}

	slog.Info("project auto-update started", "tick", s.tick)
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

		if err := s.pass(s.ctx); err != nil && s.ctx.Err() == nil {
			slog.Error("project sync pass", "error", err)
		}
	}
}

func (s *Service) pass(ctx context.Context) error {
	projects, err := s.projects.ListAutoUpdate(ctx)
	if err != nil {
		return fmt.Errorf("projects.ListAutoUpdate: %w", err)
	}

	for _, p := range projects {
		if err = s.syncProject(ctx, p); err != nil && ctx.Err() == nil {
			metricSyncErrors.Inc()
			slog.Error("project sync", "project", p.Name, "image", p.Image, "error", err)
		}
	}
	return nil
}

// syncProject сравнивает digest тега в registry с зарегистрированным и при
// расхождении перерегистрирует проект тем же путём, что и ручная
// регистрация: обновятся все даги образа разом.
func (s *Service) syncProject(ctx context.Context, p *projectModel.Main) error {
	if strings.Contains(p.Image, "@") {
		// ref задан digest'ом — новой версии у него не бывает
		return nil
	}
	current, ok := digestOf(p.ImageDigest)
	if !ok {
		// регистрация не пиннила digest (образ вне registry) — сравнивать не
		// с чем; синк был бы вечным циклом describe
		slog.Warn("project auto-update skipped: registration is not digest-pinned",
			"project", p.Name, "image", p.Image)
		return nil
	}

	latest, err := s.resolver.ResolveDigest(ctx, p.Image)
	if err != nil {
		return fmt.Errorf("resolve digest: %w", err)
	}
	if latest == current {
		return nil
	}

	slog.Info("project image changed, re-registering", "project", p.Name, "image", p.Image,
		"old_digest", current, "new_digest", latest)

	if err = s.enqueuer.EnqueueAuto(ctx, p.Name, p.Image); err != nil {
		return fmt.Errorf("enqueue re-register: %w", err)
	}
	metricSyncUpdates.Inc()
	return nil
}

// digestOf выделяет «sha256:...» из пиннутого ref (repo@sha256:...);
// false — ref без digest'а.
func digestOf(pinnedRef string) (string, bool) {
	_, digest, ok := strings.Cut(pinnedRef, "@")
	return digest, ok && digest != ""
}
