// Package dagsync — авто-обновление дагов (аналог keel poll):
// периодический digest-чек тега в registry для дагов с auto_update и полная
// перерегистрация при изменении. Сломанный новый образ запись дага не
// трогает — ошибка логируется и повторится следующим тиком.
package dagsync

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

type DagServiceI interface {
	ListAutoUpdate(ctx context.Context) ([]*dagModel.Main, error)
}

// DigestResolverI — дешёвый резолв текущего digest'а тега («sha256:...»)
// через registry API, без скачивания образа.
type DigestResolverI interface {
	ResolveDigest(ctx context.Context, image string) (string, error)
}

// RegistrarI — обычная полная перерегистрация дага (describe → валидация →
// сохранение); autoUpdate nil — не трогать флаг.
type RegistrarI interface {
	Register(ctx context.Context, image string, autoUpdate *bool) (*dagModel.Main, error)
}

type Service struct {
	dagSvc    DagServiceI
	resolver  DigestResolverI
	registrar RegistrarI
	tick      time.Duration

	ctx       context.Context
	ctxCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(dagSvc DagServiceI, resolver DigestResolverI, registrar RegistrarI, tick time.Duration) *Service {
	ctx, cancel := context.WithCancel(context.Background())

	return &Service{
		dagSvc:    dagSvc,
		resolver:  resolver,
		registrar: registrar,
		tick:      tick,
		ctx:       ctx,
		ctxCancel: cancel,
	}
}

// Start запускает цикл синка; tick <= 0 — авто-обновление выключено.
func (s *Service) Start() {
	if s.tick <= 0 {
		slog.Info("dag auto-update disabled (DAG_SYNC_TICK=0)")
		return
	}

	slog.Info("dag auto-update started", "tick", s.tick)
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
			slog.Error("dag sync pass", "error", err)
		}
	}
}

func (s *Service) pass(ctx context.Context) error {
	dags, err := s.dagSvc.ListAutoUpdate(ctx)
	if err != nil {
		return fmt.Errorf("dagSvc.ListAutoUpdate: %w", err)
	}

	for _, dag := range dags {
		if err = s.syncDag(ctx, dag); err != nil && ctx.Err() == nil {
			metricSyncErrors.Inc()
			slog.Error("dag sync", "dag", dag.Name, "image", dag.Image, "error", err)
		}
	}
	return nil
}

// syncDag сравнивает digest тега в registry с зарегистрированным и при
// расхождении перерегистрирует даг тем же путём, что и ручная регистрация.
func (s *Service) syncDag(ctx context.Context, dag *dagModel.Main) error {
	if strings.Contains(dag.Image, "@") {
		// ref задан digest'ом — новой версии у него не бывает
		return nil
	}
	current, ok := digestOf(dag.ImageDigest)
	if !ok {
		// регистрация не пиннила digest (образ вне registry) — сравнивать не
		// с чем; синк был бы вечным циклом describe
		slog.Warn("dag auto-update skipped: registration is not digest-pinned", "dag", dag.Name, "image", dag.Image)
		return nil
	}

	latest, err := s.resolver.ResolveDigest(ctx, dag.Image)
	if err != nil {
		return fmt.Errorf("resolve digest: %w", err)
	}
	if latest == current {
		return nil
	}

	slog.Info("dag image changed, re-registering", "dag", dag.Name, "image", dag.Image,
		"old_digest", current, "new_digest", latest)

	if _, err = s.registrar.Register(ctx, dag.Image, nil); err != nil {
		return fmt.Errorf("re-register: %w", err)
	}
	metricSyncUpdates.Inc()

	slog.Info("dag auto-updated", "dag", dag.Name, "image", dag.Image, "digest", latest)
	return nil
}

// digestOf выделяет «sha256:...» из пиннутого ref (repo@sha256:...);
// false — ref без digest'а.
func digestOf(pinnedRef string) (string, bool) {
	_, digest, ok := strings.Cut(pinnedRef, "@")
	return digest, ok && digest != ""
}
