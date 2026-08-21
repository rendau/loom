// Интеграционные тесты control plane: полный цикл планировщика против
// настоящего Postgres — триггер рана, очередь (FOR UPDATE SKIP LOCKED),
// обычные и стримовые рёбра, финализация попыток и лог-стримы. Executor и
// artifact-клиент — фейки, управляемые тестом.
//
// Требуется Postgres: TEST_PG_DSN=postgres://...; без него тесты скипаются.
package test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mechta-market/mobone/v2"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	loom "github.com/rendau/loom/sdk"
	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	dagDb "github.com/rendau/loom/server/internal/domain/dag/repo/db"
	dagService "github.com/rendau/loom/server/internal/domain/dag/service"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	runDb "github.com/rendau/loom/server/internal/domain/run/repo/db"
	runService "github.com/rendau/loom/server/internal/domain/run/service"
	domainScheduler "github.com/rendau/loom/server/internal/domain/scheduler"
	domainTasklog "github.com/rendau/loom/server/internal/domain/tasklog"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
	runUsc "github.com/rendau/loom/server/internal/usecase/run"
)

const waitTimeout = 5 * time.Second

// ── фейки ───────────────────────────────────────────────

type fakeExecutor struct {
	mu         sync.Mutex
	launches   []runModel.LaunchSpec
	failLaunch map[string]bool // task → Launch возвращает ошибку
	alive      map[runModel.AttemptRef]bool
	events     chan runModel.ExecEvent
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		failLaunch: map[string]bool{},
		alive:      map[runModel.AttemptRef]bool{},
		events:     make(chan runModel.ExecEvent, 100),
	}
}

func (e *fakeExecutor) Launch(_ context.Context, spec runModel.LaunchSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.failLaunch[spec.Ref.Task] {
		return fmt.Errorf("no capacity")
	}
	e.launches = append(e.launches, spec)
	e.alive[spec.Ref] = true
	return nil
}

func (e *fakeExecutor) Kill(_ context.Context, ref runModel.AttemptRef) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.alive, ref)
	return nil
}

func (e *fakeExecutor) ListAlive(context.Context) ([]runModel.AttemptRef, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lo.Keys(e.alive), nil
}

// vanish имитирует исчезновение Job'а попытки (рестарт server между claim и
// Launch, TTL-очистка, ручное удаление) — зомби для reconcile-детекта.
func (e *fakeExecutor) vanish(ref runModel.AttemptRef) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.alive, ref)
}

func (e *fakeExecutor) Events() <-chan runModel.ExecEvent { return e.events }

func (e *fakeExecutor) launched(task string) (runModel.LaunchSpec, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lo.Find(e.launches, func(s runModel.LaunchSpec) bool { return s.Ref.Task == task })
}

func (e *fakeExecutor) launchedAttempt(task string, attempt int32) (runModel.LaunchSpec, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lo.Find(e.launches, func(s runModel.LaunchSpec) bool {
		return s.Ref.Task == task && s.Ref.Attempt == attempt
	})
}

func (e *fakeExecutor) started(ref runModel.AttemptRef) {
	e.events <- runModel.ExecEvent{Ref: ref, Type: runModel.ExecEventStarted}
}

func (e *fakeExecutor) finished(ref runModel.AttemptRef, success bool, exitCode int32, reason string) {
	e.events <- runModel.ExecEvent{Ref: ref, Type: runModel.ExecEventFinished, Exit: &runModel.ExitInfo{
		Success:  success,
		ExitCode: &exitCode,
		Reason:   reason,
	}}
}

type fakeArtifact struct {
	mu       sync.Mutex
	finished []runModel.AttemptRef
}

func (a *fakeArtifact) FinishAttempt(_ context.Context, ref runModel.AttemptRef) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finished = append(a.finished, ref)
	return nil
}

func (a *fakeArtifact) finishedRefs() []runModel.AttemptRef {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.finished)
}

// ── окружение ───────────────────────────────────────────

type env struct {
	pool       *pgxpool.Pool
	dagSvc     *dagService.Service
	runSvc     *runService.Service
	tasklogSvc *domainTasklog.Service
	executor   *fakeExecutor
	artifact   *fakeArtifact
	scheduler  *domainScheduler.Scheduler
	runUsecase *runUsc.Usecase
}

func newEnv(t *testing.T) *env {
	t.Helper()

	dsn := os.Getenv("TEST_PG_DSN")
	if dsn == "" {
		t.Skip("TEST_PG_DSN is not set")
	}

	m, err := migrate.New("file://../migrations", dsn)
	require.NoError(t, err)
	if err = m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatal(err)
	}

	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	_, err = pool.Exec(context.Background(), `TRUNCATE attempt, task_instance, run, dag`)
	require.NoError(t, err)

	txm := mobone.NewTransactionManager(pool)
	base := commonRepoPg.NewBase(pool, txm)

	dagSvc := dagService.New(dagDb.New(base))
	runSvc := runService.New(runDb.New(base), txm)

	tasklogSvc, err := domainTasklog.New(t.TempDir())
	require.NoError(t, err)

	executor := newFakeExecutor()
	artifact := &fakeArtifact{}

	scheduler := domainScheduler.New(runSvc, dagSvc, executor, artifact, tasklogSvc,
		domainScheduler.Config{
			Tick:          30 * time.Millisecond,
			CronTick:      50 * time.Millisecond,
			ReconcileTick: 30 * time.Millisecond,
			ZombieGrace:   50 * time.Millisecond,
			ClaimLimit:    10,
			TaskEnv:       domainScheduler.TaskEnv{ArtifactAddr: "artifact:5051", ServerAddr: "server:5052"},
		})
	scheduler.Start()
	t.Cleanup(scheduler.Stop)

	return &env{
		pool:       pool,
		dagSvc:     dagSvc,
		runSvc:     runSvc,
		tasklogSvc: tasklogSvc,
		executor:   executor,
		artifact:   artifact,
		scheduler:  scheduler,
		runUsecase: runUsc.New(runSvc, dagSvc, scheduler),
	}
}

// registerDag регистрирует даг по сырому манифесту (как из `describe`).
func (e *env) registerDag(t *testing.T, rawManifest string) string {
	t.Helper()

	m, err := manifest.Parse([]byte(rawManifest))
	require.NoError(t, err)

	dag, err := e.dagSvc.Register(context.Background(), "registry/"+m.Name+":latest",
		"registry/"+m.Name+"@sha256:deadbeef", []byte(rawManifest), m)
	require.NoError(t, err)

	return dag.Name
}

func (e *env) taskStatuses(t *testing.T, runId string) map[string]string {
	t.Helper()
	tis, err := e.runSvc.ListTaskInstances(context.Background(), runId)
	require.NoError(t, err)
	return lo.SliceToMap(tis, func(ti *runModel.TaskInstance) (string, string) { return ti.Task, ti.Status })
}

func (e *env) runStatus(t *testing.T, runId string) string {
	t.Helper()
	run, _, err := e.runSvc.Get(context.Background(), runId, true)
	require.NoError(t, err)
	return run.Status
}

func (e *env) waitLaunched(t *testing.T, task string) runModel.LaunchSpec {
	t.Helper()
	require.Eventually(t, func() bool {
		_, ok := e.executor.launched(task)
		return ok
	}, waitTimeout, 10*time.Millisecond, "task %q was not launched", task)

	spec, _ := e.executor.launched(task)
	return spec
}

func (e *env) waitLaunchedAttempt(t *testing.T, task string, attempt int32) runModel.LaunchSpec {
	t.Helper()
	require.Eventually(t, func() bool {
		_, ok := e.executor.launchedAttempt(task, attempt)
		return ok
	}, waitTimeout, 10*time.Millisecond, "attempt %d of task %q was not launched", attempt, task)

	spec, _ := e.executor.launchedAttempt(task, attempt)
	return spec
}

func (e *env) waitRunStatus(t *testing.T, runId, status string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return e.runStatus(t, runId) == status
	}, waitTimeout, 10*time.Millisecond, "run %q did not reach status %q", runId, status)
}

func readLog(t *testing.T, svc *domainTasklog.Service, ref runModel.AttemptRef) []tasklogModel.Entry {
	t.Helper()
	var got []tasklogModel.Entry
	err := svc.Read(context.Background(),
		tasklogModel.AttemptKey{RunId: ref.RunId, Task: ref.Task, Attempt: ref.Attempt},
		false, func(entries []tasklogModel.Entry) error {
			got = append(got, entries...)
			return nil
		})
	require.NoError(t, err)
	return got
}

// ── тесты ───────────────────────────────────────────────

const etlManifest = `{
	"sdk_version": "0.1.0",
	"name": "demo-etl",
	"tasks": [
		{"name": "extract"},
		{"name": "transform", "depends_on": [{"task": "extract", "streamed": true}]},
		{"name": "load", "depends_on": [{"task": "transform"}]}
	]
}`

// Полный happy path: стримовый получатель ко-стартует с отправителем,
// обычное ребро ждёт успеха, ран завершается success, попытки финализированы,
// логи закоммичены со строкой control plane.
func TestSchedulerHappyPathWithStreamedEdge(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, etlManifest)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	// корень уходит в запуск
	extract := e.waitLaunched(t, "extract")
	assert.Equal(t, int32(1), extract.Ref.Attempt)
	assert.Equal(t, "registry/demo-etl@sha256:deadbeef", extract.Image)
	assert.Equal(t, runId, extract.Env[loom.EnvRunID])
	assert.Equal(t, "extract", extract.Env[loom.EnvTask])
	assert.Equal(t, "1", extract.Env[loom.EnvAttempt])
	assert.Equal(t, "{}", extract.Env[loom.EnvDepAttempts])
	assert.Equal(t, "artifact:5051", extract.Env[loom.EnvArtifactAddr])
	assert.Equal(t, "server:5052", extract.Env[loom.EnvServerAddr])

	// стримовый получатель ко-стартует со стартом отправителя
	e.executor.started(extract.Ref)
	transform := e.waitLaunched(t, "transform")
	assert.Equal(t, `{"extract":1}`, transform.Env[loom.EnvDepAttempts])

	// обычное ребро (load после transform) ждёт успеха отправителя
	e.executor.started(transform.Ref)
	e.executor.finished(extract.Ref, true, 0, "")
	time.Sleep(150 * time.Millisecond)
	_, loadLaunched := e.executor.launched("load")
	assert.False(t, loadLaunched, "load не должен стартовать до успеха transform")

	e.executor.finished(transform.Ref, true, 0, "")
	load := e.waitLaunched(t, "load")
	assert.Equal(t, `{"transform":1}`, load.Env[loom.EnvDepAttempts])

	e.executor.started(load.Ref)
	e.executor.finished(load.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	statuses := e.taskStatuses(t, runId)
	assert.Equal(t, map[string]string{
		"extract":   runModel.TaskStatusSuccess,
		"transform": runModel.TaskStatusSuccess,
		"load":      runModel.TaskStatusSuccess,
	}, statuses)

	// страховочный FinishAttempt ушёл на artifact-сервер по каждой попытке
	assert.ElementsMatch(t,
		[]runModel.AttemptRef{extract.Ref, transform.Ref, load.Ref},
		e.artifact.finishedRefs())

	// лог каждой попытки закоммичен и содержит строку control plane об исходе
	for _, ref := range []runModel.AttemptRef{extract.Ref, transform.Ref, load.Ref} {
		entries := readLog(t, e.tasklogSvc, ref)
		require.NotEmpty(t, entries)
		last := entries[len(entries)-1]
		assert.Equal(t, tasklogModel.SourceServer, last.Source)
		assert.Contains(t, last.Line, "attempt succeeded")
	}

	// в деталях рана — попытки с exit-информацией
	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 3)
	for _, a := range attempts {
		assert.Equal(t, runModel.AttemptStatusSuccess, a.Status)
		require.NotNil(t, a.ExitCode)
		assert.Equal(t, int32(0), *a.ExitCode)
		assert.False(t, a.FinishedAt.IsZero())
	}
}

// Падение отправителя каскадно валит pending-потомков в upstream_failed,
// ран — failed, exit-информация сохранена.
func TestSchedulerFailureCascade(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "fail-cascade",
		"tasks": [
			{"name": "a"},
			{"name": "b", "depends_on": [{"task": "a"}]},
			{"name": "c", "depends_on": [{"task": "b"}]}
		]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	e.executor.started(a.Ref)
	e.executor.finished(a.Ref, false, 137, "OOMKilled")

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)

	statuses := e.taskStatuses(t, runId)
	assert.Equal(t, map[string]string{
		"a": runModel.TaskStatusFailed,
		"b": runModel.TaskStatusUpstreamFailed,
		"c": runModel.TaskStatusUpstreamFailed,
	}, statuses)

	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.NotNil(t, attempts[0].ExitCode)
	assert.Equal(t, int32(137), *attempts[0].ExitCode)
	assert.Equal(t, "OOMKilled", attempts[0].ExitReason)

	// причина смерти дописана в лог попытки
	entries := readLog(t, e.tasklogSvc, a.Ref)
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[len(entries)-1].Line, "OOMKilled")
}

// Ошибка Launch фиксируется как проваленная попытка с reason=launch_failed.
func TestSchedulerLaunchFailure(t *testing.T) {
	e := newEnv(t)
	e.executor.failLaunch["a"] = true

	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "launch-fail",
		"tasks": [{"name": "a"}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)

	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, runModel.AttemptStatusFailed, attempts[0].Status)
	assert.Equal(t, "launch_failed", attempts[0].ExitReason)
}

// Дубли finished-событий (resync informer'а) схлопываются идемпотентной
// финализацией: попытка финализируется один раз.
func TestSchedulerDuplicateEventsIdempotent(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "dup-events",
		"tasks": [{"name": "a"}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	e.executor.started(a.Ref)
	e.executor.finished(a.Ref, true, 0, "")
	e.executor.finished(a.Ref, false, 1, "late duplicate")
	e.executor.finished(a.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, runModel.AttemptStatusSuccess, attempts[0].Status)
	require.NotNil(t, attempts[0].ExitCode)
	assert.Equal(t, int32(0), *attempts[0].ExitCode)

	// FinishAttempt на artifact-сервере ушёл ровно один раз
	assert.Equal(t, []runModel.AttemptRef{a.Ref}, e.artifact.finishedRefs())
}

// Упавшая попытка таска с retries уходит в up_for_retry и после backoff'а
// перезапускается новой попыткой; успех ретрая закрывает ран как success.
func TestSchedulerRetrySucceeds(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "retry-ok",
		"tasks": [{"name": "a", "retries": 1, "retry_delay_sec": 1}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	first := e.waitLaunchedAttempt(t, "a", 1)
	e.executor.started(first.Ref)
	e.executor.finished(first.Ref, false, 1, "Error")

	// попытка провалена, но таск ждёт ретрая, ран продолжается
	require.Eventually(t, func() bool {
		return e.taskStatuses(t, runId)["a"] == runModel.TaskStatusUpForRetry
	}, waitTimeout, 10*time.Millisecond)
	assert.Equal(t, runModel.RunStatusRunning, e.runStatus(t, runId))

	tis, err := e.runSvc.ListTaskInstances(context.Background(), runId)
	require.NoError(t, err)
	assert.False(t, tis[0].RetryAt.IsZero(), "retry_at должен быть назначен")
	assert.True(t, tis[0].FinishedAt.IsZero(), "up_for_retry — не терминальный статус")

	// в логе первой попытки — строка о запланированном ретрае
	entries := readLog(t, e.tasklogSvc, first.Ref)
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[len(entries)-1].Line, "retry scheduled at")

	// после backoff'а стартует вторая попытка
	second := e.waitLaunchedAttempt(t, "a", 2)
	assert.Equal(t, "2", second.Env[loom.EnvAttempt])
	e.executor.started(second.Ref)
	e.executor.finished(second.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
}

// Ретраи исчерпаны — таск и ран падают, попыток ровно retries+1.
func TestSchedulerRetriesExhausted(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "retry-fail",
		"tasks": [{"name": "a", "retries": 1, "retry_delay_sec": 1}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	for attempt := int32(1); attempt <= 2; attempt++ {
		spec := e.waitLaunchedAttempt(t, "a", attempt)
		e.executor.started(spec.Ref)
		e.executor.finished(spec.Ref, false, 1, "Error")
	}

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)
	assert.Equal(t, runModel.TaskStatusFailed, e.taskStatuses(t, runId)["a"])

	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
}

// Попытка, работающая дольше timeout_sec, убивается сторожевым таймером
// планировщика и финализируется с reason=timeout.
func TestSchedulerTaskTimeout(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "timeout-dag",
		"tasks": [{"name": "a", "timeout_sec": 1}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	e.executor.started(a.Ref)
	// finished-события нет — попытка «зависла», её добьёт watchdog

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)

	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, runModel.AttemptStatusFailed, attempts[0].Status)
	assert.Equal(t, "timeout", attempts[0].ExitReason)

	entries := readLog(t, e.tasklogSvc, a.Ref)
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[len(entries)-1].Line, "timeout")
}

// Зомби-детект: попытка, потерянная до старта (Job исчез, started-события
// не было — например, server упал между claim и Launch), перезапускается
// немедленно и не сжигает ретраи (у таска их нет вовсе).
func TestSchedulerZombieLostBeforeStart(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "zombie-start",
		"tasks": [{"name": "a"}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	first := e.waitLaunchedAttempt(t, "a", 1)
	e.executor.vanish(first.Ref) // Job пропал, попытка так и висит в starting

	// reconcile вернул таск в очередь: вторая попытка — при retries=0
	second := e.waitLaunchedAttempt(t, "a", 2)
	e.executor.started(second.Ref)
	e.executor.finished(second.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 2)

	lost, ok := lo.Find(attempts, func(a *runModel.Attempt) bool { return a.Attempt == 1 })
	require.True(t, ok)
	assert.Equal(t, runModel.AttemptStatusFailed, lost.Status)
	assert.Equal(t, "lost", lost.ExitReason)
}

// Зомби-детект: под, исчезнувший на бегу без finished-события (нода умерла,
// событие потеряно), финализируется по обычной политике ретраев — без них
// таск и ран падают.
func TestSchedulerZombieRunningPodLost(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "zombie-run",
		"tasks": [{"name": "a"}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName)
	require.NoError(t, err)

	a := e.waitLaunchedAttempt(t, "a", 1)
	e.executor.started(a.Ref)
	e.executor.vanish(a.Ref) // Job пропал на бегу

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)

	_, _, attempts, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, "pod_lost", attempts[0].ExitReason)

	// причина зафиксирована и в логе попытки
	entries := readLog(t, e.tasklogSvc, a.Ref)
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[len(entries)-1].Line, "pod_lost")
}

// Cron-триггер: у дага с расписанием наступивший next_run_at рождает ровно
// один ран с trigger=schedule, а next_run_at сдвигается в будущее.
func TestSchedulerCronTrigger(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "cron-dag",
		"schedule": "* * * * *",
		"tasks": [{"name": "a"}]
	}`)

	// регистрация назначила ближайшее срабатывание в будущем
	dag, _, err := e.dagSvc.Get(context.Background(), dagName, true)
	require.NoError(t, err)
	require.False(t, dag.NextRunAt.IsZero())
	assert.True(t, dag.NextRunAt.After(time.Now()))

	// наступление срабатывания имитируем сдвигом next_run_at в прошлое
	_, err = e.pool.Exec(context.Background(),
		`UPDATE dag SET next_run_at = now() - interval '1 second' WHERE name = $1`, dagName)
	require.NoError(t, err)

	var runs []*runModel.Main
	require.Eventually(t, func() bool {
		runs, _, err = e.runSvc.List(context.Background(),
			&runModel.ListReq{DagName: &dagName})
		require.NoError(t, err)
		return len(runs) > 0
	}, waitTimeout, 10*time.Millisecond, "cron не создал ран")

	require.Len(t, runs, 1)
	assert.Equal(t, runModel.TriggerSchedule, runs[0].Trigger)

	// next_run_at сдвинут вперёд — повторного триггера этого тика не будет
	dag, _, err = e.dagSvc.Get(context.Background(), dagName, true)
	require.NoError(t, err)
	assert.True(t, dag.NextRunAt.After(time.Now()))

	time.Sleep(200 * time.Millisecond) // несколько cron-проходов
	runs, _, err = e.runSvc.List(context.Background(), &runModel.ListReq{DagName: &dagName})
	require.NoError(t, err)
	assert.Len(t, runs, 1, "cron не должен дублировать ран")

	// пауза дага останавливает расписание: наступивший next_run_at не триггерит
	require.NoError(t, e.dagSvc.SetPaused(context.Background(), dagName, true))
	_, err = e.pool.Exec(context.Background(),
		`UPDATE dag SET next_run_at = now() - interval '1 second' WHERE name = $1`, dagName)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)
	runs, _, err = e.runSvc.List(context.Background(), &runModel.ListReq{DagName: &dagName})
	require.NoError(t, err)
	assert.Len(t, runs, 1, "пауза должна останавливать cron-запуски")

	// снятие с паузы пересчитывает next_run_at от текущего момента (без catchup)
	require.NoError(t, e.dagSvc.SetPaused(context.Background(), dagName, false))
	dag, _, err = e.dagSvc.Get(context.Background(), dagName, true)
	require.NoError(t, err)
	assert.True(t, dag.NextRunAt.After(time.Now()))
}
