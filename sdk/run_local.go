package loom

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"golang.org/x/sync/errgroup"
)

// defaultLocalDir — каталог локальных ранов относительно рабочей директории.
const defaultLocalDir = ".loom/runs"

type localCfg struct {
	dir string
}

type LocalOption func(*localCfg)

// LocalDir переопределяет каталог, куда локальный ран пишет артефакты.
func LocalDir(dir string) LocalOption {
	return func(c *localCfg) { c.dir = dir }
}

// RunLocal выполняет весь даг в текущем процессе: таски — горутинами,
// артефакты — файлами в <dir>/<run-id>/ (по умолчанию .loom/runs/). Данные
// остаются после рана — промежуточные артефакты можно изучать. Семантика
// рёбер и хранилища та же, что в распределённом режиме: обычная зависимость
// ждёт успеха, стримовая — старта.
func (d *DAG) RunLocal(ctx context.Context, opts ...LocalOption) error {
	if err := d.Validate(); err != nil {
		return err
	}

	cfg := localCfg{dir: defaultLocalDir}
	for _, opt := range opts {
		opt(&cfg)
	}

	// таймстампный run-id: раны не конфликтуют между собой (артефакты
	// скоупятся на попытку, повторная запись той же попытки запрещена)
	runID := "local-" + time.Now().Format("20060102-150405.000")

	store, err := newFsStore(cfg.dir)
	if err != nil {
		return fmt.Errorf("init local artifact store: %w", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("dag", d.name, "run_id", runID)
	runDir := filepath.Join(cfg.dir, runID)
	log.Info("local run started", "artifacts_dir", runDir)

	started := make(map[string]chan struct{}, len(d.order))
	succeeded := make(map[string]chan struct{}, len(d.order))
	for _, name := range d.order {
		started[name] = make(chan struct{})
		succeeded[name] = make(chan struct{})
	}

	g, ctx := errgroup.WithContext(ctx)
	for _, name := range d.order {
		t := d.tasks[name]
		g.Go(func() error {
			for _, dep := range t.deps {
				wait := succeeded[dep.task.name]
				if dep.streamed {
					wait = started[dep.task.name]
				}
				select {
				case <-wait:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			close(started[t.name])

			taskLog := log.With("task", t.name)

			taskCtx, taskCancel := taskContext(ctx, t)
			defer taskCancel()

			rt := newRuntime(taskCtx, t, runID, 1, taskLog, store, nil)

			taskLog.Info("task started")
			startedAt := time.Now()

			err := runTaskFn(taskCtx, t, rt)
			finErr := rt.finish(err)
			faErr := store.finishAttempt(runID, t.name, 1)
			if err == nil {
				err = errors.Join(finErr, faErr)
			}

			if err != nil {
				taskLog.Error("task failed", "error", err, "duration", time.Since(startedAt))
				return fmt.Errorf("task %q: %w", t.name, err)
			}

			taskLog.Info("task succeeded", "duration", time.Since(startedAt))
			close(succeeded[t.name])

			return nil
		})
	}

	err = g.Wait()
	log.Info("local run finished", "success", err == nil, "artifacts_dir", runDir)

	return err
}

// taskContext навешивает Timeout таска на контекст попытки; без таймаута —
// контекст как есть.
func taskContext(ctx context.Context, t *Task) (context.Context, context.CancelFunc) {
	if t.timeout > 0 {
		return context.WithTimeoutCause(ctx, t.timeout,
			fmt.Errorf("task %q timeout after %s", t.name, t.timeout))
	}
	return ctx, func() {}
}

// runTaskFn выполняет тело таска, конвертируя panic в ошибку.
func runTaskFn(ctx context.Context, t *Task, rt *Runtime) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic: %v\n%s", recovered, debug.Stack())
		}
	}()

	return t.fn(ctx, rt)
}
