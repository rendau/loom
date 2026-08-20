// Package scheduler — раскрутка графа рана: очередь тасков в Postgres
// (FOR UPDATE SKIP LOCKED, решение №10), запуск попыток через executor,
// обработка событий их жизненного цикла и финализация (FinishAttempt на
// artifact-сервере, закрытие лог-стрима, каскад по рёбрам графа).
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"github.com/samber/lo"

	loom "github.com/rendau/loom/sdk"
	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
)

// finalizeTimeout — на пост-обработку завершения попытки (FinishAttempt на
// artifact-сервере, закрытие лог-стрима) отдельным от событийного цикла
// контекстом.
const finalizeTimeout = 15 * time.Second

// TaskEnv — адреса data/control plane, какими их видят поды тасков;
// попадают в env-контракт LOOM_* контейнера.
type TaskEnv struct {
	ArtifactAddr string
	ServerAddr   string
}

type Scheduler struct {
	runSvc     RunServiceI
	executor   ExecutorI
	artifact   ArtifactI
	tasklog    TaskLogI
	tick       time.Duration
	claimLimit int64
	taskEnv    TaskEnv

	nudgeCh   chan struct{}
	ctx       context.Context
	ctxCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(runSvc RunServiceI, executor ExecutorI, artifact ArtifactI, tasklog TaskLogI, tick time.Duration, claimLimit int64, taskEnv TaskEnv) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		runSvc:     runSvc,
		executor:   executor,
		artifact:   artifact,
		tasklog:    tasklog,
		tick:       tick,
		claimLimit: claimLimit,
		taskEnv:    taskEnv,
		nudgeCh:    make(chan struct{}, 1),
		ctx:        ctx,
		ctxCancel:  cancel,
	}
}

func (s *Scheduler) Start() {
	s.wg.Go(s.loop)
	s.wg.Go(s.listenEvents)
}

func (s *Scheduler) Stop() {
	s.ctxCancel()
	s.wg.Wait()
}

// Nudge будит планировщик раньше тика (триггер рана, событие попытки).
func (s *Scheduler) Nudge() {
	select {
	case s.nudgeCh <- struct{}{}:
	default:
	}
}

func (s *Scheduler) loop() {
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
			slog.Error("scheduler pass", "error", err)
		}
	}
}

// pass — один проход планировщика: раскрутка графов активных ранов, затем
// забор готовых тасков из очереди и запуск попыток.
func (s *Scheduler) pass(ctx context.Context) error {
	runs, err := s.runSvc.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("runSvc.ListActive: %w", err)
	}

	for _, run := range runs {
		if err = s.replanRun(ctx, run); err != nil {
			slog.Error("scheduler replan run", "run_id", run.Id, "error", err)
		}
	}

	claimed, err := s.runSvc.ClaimQueued(ctx, s.claimLimit)
	if err != nil {
		return fmt.Errorf("runSvc.ClaimQueued: %w", err)
	}

	for _, c := range claimed {
		if err = s.launch(ctx, c); err != nil {
			ref := runModel.AttemptRef{RunId: c.RunId, Task: c.Task, Attempt: c.Attempt}
			slog.Error("scheduler launch", "run_id", c.RunId, "task", c.Task, "attempt", c.Attempt, "error", err)
			s.finalize(ref, runModel.ExitInfo{
				Success: false,
				Reason:  "launch_failed",
				Message: err.Error(),
			})
		}
	}

	// запуски перевели таски в starting — стримовые потомки могли стать
	// готовыми; дорасткрутит следующий проход (Nudge из finalize/launch не
	// нужен: очередной тик и события executor'а покрывают это)
	return nil
}

func (s *Scheduler) replanRun(ctx context.Context, run *runModel.Main) error {
	m, err := manifest.Parse(run.Manifest)
	if err != nil {
		return fmt.Errorf("parse run manifest: %w", err)
	}

	tis, err := s.runSvc.ListTaskInstances(ctx, run.Id)
	if err != nil {
		return fmt.Errorf("runSvc.ListTaskInstances: %w", err)
	}
	statuses := lo.SliceToMap(tis, func(ti *runModel.TaskInstance) (string, string) {
		return ti.Task, ti.Status
	})

	p := buildPlan(m.Tasks, statuses)

	if len(p.UpstreamFailed) > 0 {
		err = s.runSvc.PromoteTasks(ctx, run.Id, p.UpstreamFailed, runModel.TaskStatusPending, runModel.TaskStatusUpstreamFailed)
		if err != nil {
			return fmt.Errorf("runSvc.PromoteTasks(upstream_failed): %w", err)
		}
	}

	if len(p.Promote) > 0 {
		err = s.runSvc.PromoteTasks(ctx, run.Id, p.Promote, runModel.TaskStatusPending, runModel.TaskStatusQueued)
		if err != nil {
			return fmt.Errorf("runSvc.PromoteTasks(queued): %w", err)
		}
	}

	if p.RunDone {
		if err = s.runSvc.FinishRun(ctx, run.Id, p.RunStatus); err != nil {
			return fmt.Errorf("runSvc.FinishRun: %w", err)
		}
		slog.Info("run finished", "run_id", run.Id, "status", p.RunStatus)
	}

	return nil
}

// launch запускает попытку забранного из очереди таска: собирает env-контракт
// LOOM_* (включая номера попыток зависимостей) и отдаёт executor'у.
func (s *Scheduler) launch(ctx context.Context, c runModel.ClaimedTask) error {
	run, _, err := s.runSvc.Get(ctx, c.RunId, true)
	if err != nil {
		return fmt.Errorf("runSvc.Get: %w", err)
	}

	m, err := manifest.Parse(run.Manifest)
	if err != nil {
		return fmt.Errorf("parse run manifest: %w", err)
	}

	task, ok := lo.Find(m.Tasks, func(t dagModel.Task) bool { return t.Name == c.Task })
	if !ok {
		return fmt.Errorf("task %q not in run manifest", c.Task)
	}

	tis, err := s.runSvc.ListTaskInstances(ctx, c.RunId)
	if err != nil {
		return fmt.Errorf("runSvc.ListTaskInstances: %w", err)
	}
	tiAttempts := lo.SliceToMap(tis, func(ti *runModel.TaskInstance) (string, int32) {
		return ti.Task, ti.Attempt
	})

	depAttempts := lo.SliceToMap(task.DependsOn, func(d dagModel.Dep) (string, int32) {
		return d.Task, tiAttempts[d.Task]
	})
	depAttemptsJson, err := json.Marshal(depAttempts)
	if err != nil {
		return fmt.Errorf("marshal dep attempts: %w", err)
	}

	spec := runModel.LaunchSpec{
		Ref:   runModel.AttemptRef{RunId: c.RunId, Task: c.Task, Attempt: c.Attempt},
		Image: run.ImageDigest,
		Env: map[string]string{
			loom.EnvRunID:        c.RunId,
			loom.EnvTask:         c.Task,
			loom.EnvAttempt:      strconv.Itoa(int(c.Attempt)),
			loom.EnvDepAttempts:  string(depAttemptsJson),
			loom.EnvArtifactAddr: s.taskEnv.ArtifactAddr,
			loom.EnvServerAddr:   s.taskEnv.ServerAddr,
		},
	}

	if err = s.executor.Launch(ctx, spec); err != nil {
		return fmt.Errorf("executor.Launch: %w", err)
	}

	slog.Info("attempt launched", "run_id", c.RunId, "task", c.Task, "attempt", c.Attempt)
	return nil
}

// listenEvents обрабатывает события executor'а до остановки планировщика.
func (s *Scheduler) listenEvents() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case ev, ok := <-s.executor.Events():
			if !ok {
				return
			}
			s.handleEvent(ev)
		}
	}
}

func (s *Scheduler) handleEvent(ev runModel.ExecEvent) {
	switch ev.Type {
	case runModel.ExecEventStarted:
		applied, err := s.runSvc.MarkAttemptRunning(s.ctx, ev.Ref)
		if err != nil && s.ctx.Err() == nil {
			slog.Error("mark attempt running", "run_id", ev.Ref.RunId, "task", ev.Ref.Task, "attempt", ev.Ref.Attempt, "error", err)
			return
		}
		if applied {
			s.Nudge() // стримовые потомки могли стать готовыми
		}

	case runModel.ExecEventFinished:
		exit := lo.FromPtr(ev.Exit)
		s.finalize(ev.Ref, exit)
	}
}

// finalize фиксирует завершение попытки: статусы в БД, страховочный
// FinishAttempt на artifact-сервере, дописывание exit-информации в лог и
// его commit. Идемпотентен — дубли событий и страховочные вызовы схлопываются
// на FinalizeAttempt.
func (s *Scheduler) finalize(ref runModel.AttemptRef, exit runModel.ExitInfo) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), finalizeTimeout)
	defer cancel()

	applied, err := s.runSvc.FinalizeAttempt(ctx, ref, exit)
	if err != nil {
		slog.Error("finalize attempt", "run_id", ref.RunId, "task", ref.Task, "attempt", ref.Attempt, "error", err)
		return
	}
	if !applied {
		return
	}

	// страховка: SDK вызывает FinishAttempt сам, но при смерти пода вызова
	// могло не быть — повторяем, вызов идемпотентен (решение №13)
	if err = s.artifact.FinishAttempt(ctx, ref); err != nil {
		slog.Warn("artifact finish attempt", "run_id", ref.RunId, "task", ref.Task, "attempt", ref.Attempt, "error", err)
	}

	key := tasklogModel.AttemptKey{RunId: ref.RunId, Task: ref.Task, Attempt: ref.Attempt}
	if err = s.tasklog.Finish(key, []tasklogModel.Entry{exitLogEntry(exit)}); err != nil {
		slog.Warn("finish task log", "run_id", ref.RunId, "task", ref.Task, "attempt", ref.Attempt, "error", err)
	}

	slog.Info("attempt finished", "run_id", ref.RunId, "task", ref.Task, "attempt", ref.Attempt,
		"success", exit.Success, "reason", exit.Reason)

	s.Nudge()
}

// exitLogEntry — строка от control plane с исходом попытки; при смерти SDK
// вместе с процессом это единственный след причины смерти в логе (решение №7).
func exitLogEntry(exit runModel.ExitInfo) tasklogModel.Entry {
	line := "attempt failed"
	if exit.Success {
		line = "attempt succeeded"
	}
	if exit.ExitCode != nil {
		line += fmt.Sprintf(", exit code %d", *exit.ExitCode)
	}
	if exit.Reason != "" {
		line += ", reason: " + exit.Reason
	}
	if exit.Message != "" {
		line += ", " + exit.Message
	}

	return tasklogModel.Entry{
		TsUnixMs: time.Now().UnixMilli(),
		Source:   tasklogModel.SourceServer,
		Line:     line,
	}
}
