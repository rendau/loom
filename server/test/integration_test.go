// Интеграционные тесты control plane: полный цикл планировщика против
// настоящего Postgres — триггер рана, очередь (FOR UPDATE SKIP LOCKED),
// обычные и стримовые рёбра, финализация попыток и лог-стримы. Executor и
// artifact-клиент — фейки, управляемые тестом.
//
// Требуется Postgres: TEST_PG_DSN=postgres://...; без него тесты скипаются.
package test

import (
	"bytes"
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
	poolDb "github.com/rendau/loom/server/internal/domain/pool/repo/db"
	poolService "github.com/rendau/loom/server/internal/domain/pool/service"
	domainRetention "github.com/rendau/loom/server/internal/domain/retention"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	runDb "github.com/rendau/loom/server/internal/domain/run/repo/db"
	runService "github.com/rendau/loom/server/internal/domain/run/service"
	domainScheduler "github.com/rendau/loom/server/internal/domain/scheduler"
	secretDb "github.com/rendau/loom/server/internal/domain/secret/repo/db"
	secretService "github.com/rendau/loom/server/internal/domain/secret/service"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
	variableDb "github.com/rendau/loom/server/internal/domain/variable/repo/db"
	variableService "github.com/rendau/loom/server/internal/domain/variable/service"
	"github.com/rendau/loom/server/internal/errs"
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

func (e *fakeExecutor) launchedForRun(runId string) []runModel.LaunchSpec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return lo.Filter(e.launches, func(s runModel.LaunchSpec, _ int) bool { return s.Ref.RunId == runId })
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

func (e *fakeExecutor) metrics(ref runModel.AttemptRef, peakBytes int64) {
	e.events <- runModel.ExecEvent{Ref: ref, Type: runModel.ExecEventMetrics, PeakMemoryBytes: &peakBytes}
}

type fakeArtifact struct {
	mu          sync.Mutex
	finished    []runModel.AttemptRef
	deletedRuns []string
}

func (a *fakeArtifact) FinishAttempt(_ context.Context, ref runModel.AttemptRef) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finished = append(a.finished, ref)
	return nil
}

func (a *fakeArtifact) DeleteRunArtifacts(_ context.Context, runId string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deletedRuns = append(a.deletedRuns, runId)
	return nil
}

func (a *fakeArtifact) finishedRefs() []runModel.AttemptRef {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.finished)
}

func (a *fakeArtifact) deleted() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.deletedRuns)
}

// fakeTasklog — фейк лог-клиента artifact-сервера: планировщик финализирует
// им логи попыток, retention удаляет логи рана.
type fakeTasklog struct {
	mu       sync.Mutex
	finished map[tasklogModel.AttemptKey][]tasklogModel.Entry
	deleted  []string
}

func newFakeTasklog() *fakeTasklog {
	return &fakeTasklog{finished: map[tasklogModel.AttemptKey][]tasklogModel.Entry{}}
}

func (f *fakeTasklog) Finish(_ context.Context, key tasklogModel.AttemptKey, final []tasklogModel.Entry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished[key] = append(f.finished[key], final...)
	return nil
}

func (f *fakeTasklog) DeleteRunTaskLogs(_ context.Context, runId string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, runId)
	return nil
}

func (f *fakeTasklog) deletedRuns() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.deleted)
}

// ── окружение ───────────────────────────────────────────

type env struct {
	pool        *pgxpool.Pool
	dagSvc      *dagService.Service
	runSvc      *runService.Service
	poolSvc     *poolService.Service
	secretSvc   *secretService.Service
	variableSvc *variableService.Service
	tasklog     *fakeTasklog
	executor    *fakeExecutor
	artifact    *fakeArtifact
	scheduler   *domainScheduler.Scheduler
	runUsecase  *runUsc.Usecase
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

	_, err = pool.Exec(context.Background(),
		`TRUNCATE attempt, run_value, run_env, task_instance, run, dag, dag_registration, pool, secret,
			variable, app_user, session, user_dag`)
	require.NoError(t, err)

	txm := mobone.NewTransactionManager(pool)
	base := commonRepoPg.NewBase(pool, txm)

	dagSvc := dagService.New(dagDb.New(base))
	runSvc := runService.New(runDb.New(base), txm)
	poolSvc := poolService.New(poolDb.New(base))
	secretSvc, err := secretService.New(secretDb.New(base), "test-secret-key")
	require.NoError(t, err)
	variableSvc := variableService.New(variableDb.New(base))

	// сид дефолтного пула после TRUNCATE
	_, err = pool.Exec(context.Background(),
		`INSERT INTO pool (name, slots) VALUES ('default', 64) ON CONFLICT (name) DO NOTHING`)
	require.NoError(t, err)

	tasklog := newFakeTasklog()
	executor := newFakeExecutor()
	artifact := &fakeArtifact{}

	scheduler := domainScheduler.New(runSvc, dagSvc, executor, artifact, tasklog, secretSvc, variableSvc,
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
		pool:        pool,
		dagSvc:      dagSvc,
		runSvc:      runSvc,
		poolSvc:     poolSvc,
		secretSvc:   secretSvc,
		variableSvc: variableSvc,
		tasklog:     tasklog,
		executor:    executor,
		artifact:    artifact,
		scheduler:   scheduler,
		runUsecase:  runUsc.New(runSvc, dagSvc, scheduler, allowAllAuthz{}),
	}
}

// allowAllAuthz — тесты работают с usecase напрямую, без аутентификации:
// права на даг не ограничиваем (как у внутренних вызовов control plane).
type allowAllAuthz struct{}

func (allowAllAuthz) RequireDag(context.Context, string) error { return nil }

// registerDag регистрирует даг по сырому манифесту (как из `describe`).
func (e *env) registerDag(t *testing.T, rawManifest string) string {
	t.Helper()

	m, err := manifest.Parse([]byte(rawManifest))
	require.NoError(t, err)

	dag, err := e.dagSvc.Register(context.Background(), "registry/"+m.Name+":latest",
		"registry/"+m.Name+"@sha256:deadbeef", []byte(rawManifest), m, nil)
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

// readLog — финальные строки лога попытки, дописанные планировщиком при
// финализации (сам лог живёт на artifact-сервере, здесь он — фейк).
func readLog(t *testing.T, f *fakeTasklog, ref runModel.AttemptRef) []tasklogModel.Entry {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.finished[tasklogModel.AttemptKey{RunId: ref.RunId, Task: ref.Task, Attempt: ref.Attempt}])
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
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
		entries := readLog(t, e.tasklog, ref)
		require.NotEmpty(t, entries)
		last := entries[len(entries)-1]
		assert.Equal(t, tasklogModel.SourceServer, last.Source)
		assert.Contains(t, last.Line, "attempt succeeded")
	}

	// в деталях рана — попытки с exit-информацией
	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
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

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.NotNil(t, attempts[0].ExitCode)
	assert.Equal(t, int32(137), *attempts[0].ExitCode)
	assert.Equal(t, "OOMKilled", attempts[0].ExitReason)

	// причина смерти дописана в лог попытки
	entries := readLog(t, e.tasklog, a.Ref)
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	e.executor.started(a.Ref)
	e.executor.finished(a.Ref, true, 0, "")
	e.executor.finished(a.Ref, false, 1, "late duplicate")
	e.executor.finished(a.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
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
	entries := readLog(t, e.tasklog, first.Ref)
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[len(entries)-1].Line, "retry scheduled at")

	// после backoff'а стартует вторая попытка
	second := e.waitLaunchedAttempt(t, "a", 2)
	assert.Equal(t, "2", second.Env[loom.EnvAttempt])
	e.executor.started(second.Ref)
	e.executor.finished(second.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	for attempt := int32(1); attempt <= 2; attempt++ {
		spec := e.waitLaunchedAttempt(t, "a", attempt)
		e.executor.started(spec.Ref)
		e.executor.finished(spec.Ref, false, 1, "Error")
	}

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)
	assert.Equal(t, runModel.TaskStatusFailed, e.taskStatuses(t, runId)["a"])

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 2)
}

// Ручной ретрай таска на завершённом ране (RetryTask): упавший таск
// возвращается в очередь новой попыткой, его upstream_failed-подграф
// сбрасывается и раскручивается заново, ран завершается success. На
// выполняющемся ране, для несуществующего и для upstream_failed таска
// ретрай отклоняется.
func TestRetryTaskSubgraph(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "manual-retry",
		"tasks": [
			{"name": "a"},
			{"name": "b", "depends_on": [{"task": "a"}]},
			{"name": "c", "depends_on": [{"task": "b"}]}
		]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	// пока ран выполняется — ретрай недоступен
	err = e.runUsecase.RetryTask(context.Background(), runId, "a")
	assert.ErrorIs(t, err, errs.RunNotFinished)

	a1 := e.waitLaunchedAttempt(t, "a", 1)
	e.executor.started(a1.Ref)
	e.executor.finished(a1.Ref, false, 1, "Error")
	e.waitRunStatus(t, runId, runModel.RunStatusFailed)

	// несуществующий таск и не исполнявшийся (upstream_failed) — отклоняются
	err = e.runUsecase.RetryTask(context.Background(), runId, "nope")
	assert.ErrorIs(t, err, errs.TaskNotFound)
	err = e.runUsecase.RetryTask(context.Background(), runId, "b")
	assert.ErrorIs(t, err, errs.TaskNotRetryable)

	// ретрай упавшего корня реактивирует ран и раскручивает подграф заново
	require.NoError(t, e.runUsecase.RetryTask(context.Background(), runId, "a"))
	assert.Equal(t, runModel.RunStatusRunning, e.runStatus(t, runId))

	a2 := e.waitLaunchedAttempt(t, "a", 2)
	e.executor.started(a2.Ref)
	e.executor.finished(a2.Ref, true, 0, "")

	b := e.waitLaunchedAttempt(t, "b", 1)
	assert.Equal(t, `{"a":2}`, b.Env[loom.EnvDepAttempts], "downstream читает новую попытку")
	e.executor.started(b.Ref)
	e.executor.finished(b.Ref, true, 0, "")

	c := e.waitLaunchedAttempt(t, "c", 1)
	e.executor.started(c.Ref)
	e.executor.finished(c.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)
	assert.Equal(t, map[string]string{
		"a": runModel.TaskStatusSuccess,
		"b": runModel.TaskStatusSuccess,
		"c": runModel.TaskStatusSuccess,
	}, e.taskStatuses(t, runId))
}

// Ретрай успешного таска: перезапускается и сам таск, и его успешный
// downstream (он потреблял старые выходы — должен пересчитаться).
func TestRetryTaskSuccessSubgraph(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "manual-retry-ok",
		"tasks": [
			{"name": "a"},
			{"name": "b", "depends_on": [{"task": "a"}]}
		]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	for _, task := range []string{"a", "b"} {
		spec := e.waitLaunchedAttempt(t, task, 1)
		e.executor.started(spec.Ref)
		e.executor.finished(spec.Ref, true, 0, "")
	}
	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	require.NoError(t, e.runUsecase.RetryTask(context.Background(), runId, "a"))

	a2 := e.waitLaunchedAttempt(t, "a", 2)
	e.executor.started(a2.Ref)
	e.executor.finished(a2.Ref, true, 0, "")

	b2 := e.waitLaunchedAttempt(t, "b", 2)
	assert.Equal(t, `{"a":2}`, b2.Env[loom.EnvDepAttempts])
	e.executor.started(b2.Ref)
	e.executor.finished(b2.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 4, "старые попытки остаются историей")
}

// Принудительная остановка рана: живая попытка убивается через executor и
// финализируется (лог закоммичен, артефакты закрыты), не начавшиеся таски
// получают canceled, ран — canceled. Повторная остановка отклоняется, а
// остановленный ран можно доиграть ретраем.
func TestCancelRun(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "cancel-dag",
		"tasks": [
			{"name": "a", "retries": 2},
			{"name": "b", "depends_on": [{"task": "a"}]},
			{"name": "c", "depends_on": [{"task": "b"}]}
		]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a1 := e.waitLaunchedAttempt(t, "a", 1)
	e.executor.started(a1.Ref)

	require.NoError(t, e.runUsecase.Cancel(context.Background(), runId))

	assert.Equal(t, runModel.RunStatusCanceled, e.runStatus(t, runId))
	assert.Equal(t, map[string]string{
		"a": runModel.TaskStatusCanceled,
		"b": runModel.TaskStatusCanceled,
		"c": runModel.TaskStatusCanceled,
	}, e.taskStatuses(t, runId))

	alive, err := e.executor.ListAlive(context.Background())
	require.NoError(t, err)
	assert.Empty(t, alive, "попытка убита в executor'е")

	// попытка финализирована: failed с reason=canceled, ретрай не назначен
	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, runModel.AttemptStatusFailed, attempts[0].Status)
	assert.Equal(t, "canceled", attempts[0].ExitReason)

	assert.Contains(t, e.artifact.finishedRefs(), a1.Ref, "артефакты попытки закрыты")
	entries := readLog(t, e.tasklog, a1.Ref)
	require.NotEmpty(t, entries)
	assert.Contains(t, entries[len(entries)-1].Line, "canceled")

	// ретраи не «дозревают»: таск остаётся canceled
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, runModel.TaskStatusCanceled, e.taskStatuses(t, runId)["a"])

	// повторная остановка — уже нечего останавливать
	err = e.runUsecase.Cancel(context.Background(), runId)
	assert.ErrorIs(t, err, errs.RunNotRunning)

	// остановленный ран доигрывается ретраем
	require.NoError(t, e.runUsecase.RetryTask(context.Background(), runId, "a"))
	a2 := e.waitLaunchedAttempt(t, "a", 2)
	e.executor.started(a2.Ref)
	e.executor.finished(a2.Ref, true, 0, "")
	for _, task := range []string{"b", "c"} {
		spec := e.waitLaunchedAttempt(t, task, 1)
		e.executor.started(spec.Ref)
		e.executor.finished(spec.Ref, true, 0, "")
	}
	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)
}

// Остановка рана освобождает слоты пула: таск, ждавший в очереди, не
// запускается, а сразу становится canceled.
func TestCancelRunFreesQueue(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.poolSvc.Set(context.Background(), "tiny", 1))

	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "cancel-queue",
		"tasks": [
			{"name": "a", "pool": "tiny"},
			{"name": "b", "pool": "tiny"}
		]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	// единственный слот занимает первый таск, второй ждёт в очереди
	first := e.waitLaunched(t, "a")
	e.executor.started(first.Ref)

	require.NoError(t, e.runUsecase.Cancel(context.Background(), runId))

	assert.Equal(t, runModel.RunStatusCanceled, e.runStatus(t, runId))
	assert.Equal(t, map[string]string{
		"a": runModel.TaskStatusCanceled,
		"b": runModel.TaskStatusCanceled,
	}, e.taskStatuses(t, runId))

	// очередь остановленного рана не запускается даже после освобождения слота
	time.Sleep(150 * time.Millisecond)
	assert.Len(t, e.executor.launchedForRun(runId), 1)
}

// Остановка несуществующего рана — object_not_found, а не «уже завершён».
func TestCancelRunNotFound(t *testing.T) {
	e := newEnv(t)
	err := e.runUsecase.Cancel(context.Background(), "nope")
	assert.ErrorIs(t, err, errs.RunNotFound)
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	e.executor.started(a.Ref)
	// finished-события нет — попытка «зависла», её добьёт watchdog

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, runModel.AttemptStatusFailed, attempts[0].Status)
	assert.Equal(t, "timeout", attempts[0].ExitReason)

	entries := readLog(t, e.tasklog, a.Ref)
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	first := e.waitLaunchedAttempt(t, "a", 1)
	e.executor.vanish(first.Ref) // Job пропал, попытка так и висит в starting

	// reconcile вернул таск в очередь: вторая попытка — при retries=0
	second := e.waitLaunchedAttempt(t, "a", 2)
	e.executor.started(second.Ref)
	e.executor.finished(second.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
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

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a := e.waitLaunchedAttempt(t, "a", 1)
	e.executor.started(a.Ref)
	e.executor.vanish(a.Ref) // Job пропал на бегу

	e.waitRunStatus(t, runId, runModel.RunStatusFailed)

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, "pod_lost", attempts[0].ExitReason)

	// причина зафиксирована и в логе попытки
	entries := readLog(t, e.tasklog, a.Ref)
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
		"tasks": [{"name": "a"}]
	}`)
	require.NoError(t, e.dagSvc.SetSchedule(context.Background(), dagName, "* * * * *", false))

	// установка расписания назначила ближайшее срабатывание в будущем
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

// Параметры рана и логическая дата доходят до env попытки
// (LOOM_RUN_PARAMS / LOOM_LOGICAL_DATE); params сверх лимита отклоняются.
func TestRunParamsAndLogicalDate(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "params-dag",
		"tasks": [{"name": "a"}]
	}`)

	params := []byte(`{"date":"2026-08-01","limit":10}`)
	runId, err := e.runUsecase.Trigger(context.Background(), dagName, params)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	assert.JSONEq(t, string(params), a.Env[loom.EnvRunParams])

	// логическая дата ручного рана — момент триггера
	ld, err := time.Parse(time.RFC3339, a.Env[loom.EnvLogicalDate])
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now(), ld, time.Minute)

	run, _, err := e.runSvc.Get(context.Background(), runId, true)
	require.NoError(t, err)
	assert.JSONEq(t, string(params), string(run.Params))
	assert.False(t, run.LogicalDate.IsZero())

	// ран без параметров — env-переменной нет
	runId2, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		specs := e.executor.launchedForRun(runId2)
		return len(specs) == 1
	}, waitTimeout, 10*time.Millisecond)
	spec := e.executor.launchedForRun(runId2)[0]
	_, hasParams := spec.Env[loom.EnvRunParams]
	assert.False(t, hasParams)

	// params сверх лимита отклоняются
	big := append([]byte(`{"x":"`), bytes.Repeat([]byte("a"), runModel.MaxParamsSize)...)
	big = append(big, '"', '}')
	_, err = e.runUsecase.Trigger(context.Background(), dagName, big)
	assert.ErrorIs(t, err, errs.InvalidRequest)
}

// Catchup-даг наверстывает пропущенные тики расписания: по рану на каждый
// тик с logical_date=тик; unpause не сбрасывает его next_run_at.
func TestSchedulerCatchup(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "catchup-dag",
		"tasks": [{"name": "a"}]
	}`)
	require.NoError(t, e.dagSvc.SetSchedule(context.Background(), dagName, "@hourly", true))

	// имитируем простой: три часа пропущенных тиков
	var backlogStart time.Time
	err := e.pool.QueryRow(context.Background(), `
		UPDATE dag SET next_run_at = date_trunc('hour', now()) - interval '3 hours'
		WHERE name = $1 RETURNING next_run_at`, dagName).Scan(&backlogStart)
	require.NoError(t, err)

	// навёрстаны все четыре наступивших тика (-3h, -2h, -1h, -0h)
	var runs []*runModel.Main
	require.Eventually(t, func() bool {
		runs, _, err = e.runSvc.List(context.Background(), &runModel.ListReq{DagName: &dagName})
		require.NoError(t, err)
		return len(runs) >= 4
	}, waitTimeout, 10*time.Millisecond, "catchup не навёрстывает тики")

	time.Sleep(200 * time.Millisecond) // ещё несколько cron-проходов — дублей нет
	runs, _, err = e.runSvc.List(context.Background(), &runModel.ListReq{DagName: &dagName})
	require.NoError(t, err)
	require.Len(t, runs, 4)

	wantTicks := lo.Times(4, func(i int) time.Time { return backlogStart.Add(time.Duration(i) * time.Hour) })
	gotTicks := lo.Map(runs, func(r *runModel.Main, _ int) time.Time { return r.LogicalDate })
	assert.ElementsMatch(t,
		lo.Map(wantTicks, func(t time.Time, _ int) string { return t.UTC().Format(time.RFC3339) }),
		lo.Map(gotTicks, func(t time.Time, _ int) string { return t.UTC().Format(time.RFC3339) }))

	for _, r := range runs {
		assert.Equal(t, runModel.TriggerSchedule, r.Trigger)
	}

	// unpause catchup-дага сохраняет next_run_at (пропущенное наверстается);
	// расписание на паузе — тик в прошлом не триггерит
	require.NoError(t, e.dagSvc.SetPaused(context.Background(), dagName, true))
	past := time.Now().Add(-time.Hour).Truncate(time.Second).UTC()
	_, err = e.pool.Exec(context.Background(),
		`UPDATE dag SET next_run_at = $1 WHERE name = $2`, past, dagName)
	require.NoError(t, err)

	require.NoError(t, e.dagSvc.SetPaused(context.Background(), dagName, false))
	dag, _, err := e.dagSvc.Get(context.Background(), dagName, true)
	require.NoError(t, err)
	assert.True(t, dag.NextRunAt.Equal(past) || dag.NextRunAt.After(past),
		"unpause не должен сбрасывать next_run_at catchup-дага от «сейчас»")
}

// Backfill создаёт по рану на каждый тик расписания в [from, to) с
// trigger=backfill и logical_date=тик; некорректные запросы отклоняются.
func TestBackfill(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "backfill-dag",
		"tasks": [{"name": "a"}]
	}`)
	require.NoError(t, e.dagSvc.SetSchedule(context.Background(), dagName, "@daily", false))

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)
	params := []byte(`{"source":"backfill"}`)

	runIds, err := e.runUsecase.Backfill(context.Background(), dagName, from, to, params)
	require.NoError(t, err)
	require.Len(t, runIds, 3, "по рану на каждый тик @daily в периоде")

	for i, runId := range runIds {
		run, _, err := e.runSvc.Get(context.Background(), runId, true)
		require.NoError(t, err)
		assert.Equal(t, runModel.TriggerBackfill, run.Trigger)
		assert.JSONEq(t, string(params), string(run.Params))
		assert.True(t, run.LogicalDate.Equal(from.AddDate(0, 0, i)),
			"logical_date рана %d: ожидался %s, получен %s", i, from.AddDate(0, 0, i), run.LogicalDate)
	}

	// ошибки: период наоборот, слишком широкий период, даг без расписания
	_, err = e.runUsecase.Backfill(context.Background(), dagName, to, from, nil)
	assert.ErrorIs(t, err, errs.InvalidRequest)

	_, err = e.runUsecase.Backfill(context.Background(), dagName, from, from.AddDate(0, 0, 200), nil)
	assert.ErrorIs(t, err, errs.InvalidRequest)

	plain := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "backfill-no-schedule",
		"tasks": [{"name": "a"}]
	}`)
	_, err = e.runUsecase.Backfill(context.Background(), plain, from, to, nil)
	assert.ErrorIs(t, err, errs.InvalidRequest)
}

// Значения тасков (XCom): пуш от текущей попытки, перезапись по ключу,
// pull/list; пуш от устаревшей попытки и некорректные значения отклоняются.
func TestTaskValues(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "values-dag",
		"tasks": [{"name": "a"}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")

	// пуш и перезапись
	require.NoError(t, e.runUsecase.PushValue(context.Background(), a.Ref, "rows", []byte(`41`)))
	require.NoError(t, e.runUsecase.PushValue(context.Background(), a.Ref, "rows", []byte(`42`)))
	require.NoError(t, e.runUsecase.PushValue(context.Background(), a.Ref, "report", []byte(`{"ok":true}`)))

	v, err := e.runUsecase.PullValue(context.Background(), runId, "a", "rows")
	require.NoError(t, err)
	assert.JSONEq(t, `42`, string(v.Value))

	values, err := e.runUsecase.ListValues(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, values, 2)

	// отсутствующее значение
	_, err = e.runUsecase.PullValue(context.Background(), runId, "a", "missing")
	assert.ErrorIs(t, err, errs.ValueNotFound)

	// пуш от неактуальной попытки — отклоняется (зомби не перезапишет ретрай)
	stale := runModel.AttemptRef{RunId: runId, Task: "a", Attempt: a.Ref.Attempt + 1}
	err = e.runUsecase.PushValue(context.Background(), stale, "rows", []byte(`99`))
	assert.ErrorIs(t, err, errs.AttemptOutdated)

	// некорректные значения
	err = e.runUsecase.PushValue(context.Background(), a.Ref, "bad json", []byte(`1`))
	assert.ErrorIs(t, err, errs.InvalidRequest, "ключ с пробелом")
	err = e.runUsecase.PushValue(context.Background(), a.Ref, "rows", []byte(`{broken`))
	assert.ErrorIs(t, err, errs.InvalidRequest, "битый JSON")
	err = e.runUsecase.PushValue(context.Background(), a.Ref, "rows",
		bytes.Repeat([]byte("1"), runModel.MaxValueSize+1))
	assert.ErrorIs(t, err, errs.InvalidRequest, "сверх лимита")

	// удаление рана уносит значения каскадом
	e.executor.started(a.Ref)
	e.executor.finished(a.Ref, true, 0, "")
	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)
	require.NoError(t, e.runSvc.DeleteRun(context.Background(), runId))
	var count int
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM run_value WHERE run_id = $1`, runId).Scan(&count))
	assert.Zero(t, count)
}

// Пулы: таски конкурируют за слоты пула, приоритетный
// забирается первым; освобождение слота пускает следующего.
func TestPoolSlotsAndPriority(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.poolSvc.Set(context.Background(), "tiny", 1))

	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "pool-dag",
		"tasks": [
			{"name": "low", "pool": "tiny", "priority": 1},
			{"name": "high", "pool": "tiny", "priority": 5}
		]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	// один слот: первым уходит high (больший приоритет), low ждёт
	high := e.waitLaunched(t, "high")
	time.Sleep(150 * time.Millisecond)
	_, lowLaunched := e.executor.launched("low")
	assert.False(t, lowLaunched, "второй таск пула не должен стартовать при занятом слоте")

	// слот освободился — стартует low
	e.executor.started(high.Ref)
	e.executor.finished(high.Ref, true, 0, "")
	low := e.waitLaunched(t, "low")
	e.executor.started(low.Ref)
	e.executor.finished(low.Ref, true, 0, "")

	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	// валидация регистрации: несуществующий пул отклоняется
	err = e.poolSvc.CheckExist(context.Background(), []string{"default", "ghost"})
	assert.ErrorIs(t, err, errs.PoolNotFound)
	require.NoError(t, e.poolSvc.CheckExist(context.Background(), []string{"default", "tiny"}))
}

// max_active_runs: второй ран дага ждёт завершения первого.
func TestMaxActiveRuns(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "serial-dag",
		"max_active_runs": 1,
		"tasks": [{"name": "a"}]
	}`)

	run1, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)
	run2, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return len(e.executor.launchedForRun(run1)) == 1
	}, waitTimeout, 10*time.Millisecond)

	time.Sleep(150 * time.Millisecond)
	assert.Empty(t, e.executor.launchedForRun(run2), "второй ран должен ждать первого")

	first := e.executor.launchedForRun(run1)[0]
	e.executor.started(first.Ref)
	e.executor.finished(first.Ref, true, 0, "")
	e.waitRunStatus(t, run1, runModel.RunStatusSuccess)

	// место освободилось — второй ран поехал
	require.Eventually(t, func() bool {
		return len(e.executor.launchedForRun(run2)) == 1
	}, waitTimeout, 10*time.Millisecond)
	second := e.executor.launchedForRun(run2)[0]
	e.executor.started(second.Ref)
	e.executor.finished(second.Ref, true, 0, "")
	e.waitRunStatus(t, run2, runModel.RunStatusSuccess)
}

// Секреты: значение шифруется в БД и инъектится в env попытки; локальный
// скоуп дага перекрывает глобальный; отсутствующий секрет валит запуск
// (launch_failed); значение читается через GetValue.
func TestSecrets(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.secretSvc.Set(context.Background(), "", "db-password", []byte("s3cr3t-value")))

	// в БД значение зашифровано, плейнтекста нет
	var stored []byte
	require.NoError(t, e.pool.QueryRow(context.Background(),
		`SELECT value FROM secret WHERE name = 'db-password'`).Scan(&stored))
	assert.NotContains(t, string(stored), "s3cr3t-value")

	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "secret-dag",
		"tasks": [{"name": "a", "secrets": [{"env": "DB_PASSWORD", "secret": "db-password"}]}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	assert.Equal(t, "s3cr3t-value", a.Env["DB_PASSWORD"], "значение секрета в env попытки")

	e.executor.started(a.Ref)
	e.executor.finished(a.Ref, true, 0, "")
	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	// снапшот run_env: секрет — имя и скоуп-источник, значения НЕТ
	env, err := e.runSvc.ListRunEnv(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, env, 1)
	assert.Equal(t, "DB_PASSWORD", env[0].Env)
	assert.Equal(t, runModel.RunEnvKindSecret, env[0].Kind)
	assert.Equal(t, "db-password", env[0].Name)
	assert.Equal(t, "", env[0].Scope, "источник — глобальный скоуп")
	assert.Empty(t, env[0].Value, "значение секрета в снапшот не пишется")

	// локальный секрет дага перекрывает глобальный с тем же именем
	require.NoError(t, e.secretSvc.Set(context.Background(), dagName, "db-password", []byte("local-value")))
	runId2, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(e.executor.launchedForRun(runId2)) == 1
	}, waitTimeout, 10*time.Millisecond)
	a2 := e.executor.launchedForRun(runId2)[0]
	assert.Equal(t, "local-value", a2.Env["DB_PASSWORD"], "локальный скоуп перекрывает глобальный")
	e.executor.started(a2.Ref)
	e.executor.finished(a2.Ref, true, 0, "")
	e.waitRunStatus(t, runId2, runModel.RunStatusSuccess)

	// снапшот второго рана зафиксировал локальный источник
	env2, err := e.runSvc.ListRunEnv(context.Background(), runId2)
	require.NoError(t, err)
	require.Len(t, env2, 1)
	assert.Equal(t, dagName, env2[0].Scope)

	// список секретов — только метаданные, оба скоупа
	metas, err := e.secretSvc.List(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, metas, 2)

	// GetValue отдаёт расшифрованное значение точного скоупа
	value, err := e.secretSvc.GetValue(context.Background(), "", "db-password")
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t-value", string(value))
	value, err = e.secretSvc.GetValue(context.Background(), dagName, "db-password")
	require.NoError(t, err)
	assert.Equal(t, "local-value", string(value))
	_, err = e.secretSvc.GetValue(context.Background(), "", "ghost")
	assert.ErrorIs(t, err, errs.SecretNotFound)

	// отсутствующий секрет валит запуск: попытка failed с launch_failed
	ghostDag := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "ghost-secret-dag",
		"tasks": [{"name": "a", "secrets": [{"env": "TOKEN", "secret": "ghost"}]}]
	}`)
	ghostRun, err := e.runUsecase.Trigger(context.Background(), ghostDag, nil)
	require.NoError(t, err)

	e.waitRunStatus(t, ghostRun, runModel.RunStatusFailed)
	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), ghostRun)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, "launch_failed", attempts[0].ExitReason)

	// удаление секрета — по скоупу
	require.NoError(t, e.secretSvc.Delete(context.Background(), "", "db-password"))
	assert.ErrorIs(t, e.secretSvc.Delete(context.Background(), "", "db-password"), errs.SecretNotFound)
	require.NoError(t, e.secretSvc.Delete(context.Background(), dagName, "db-password"))
}

// Переменные: значение инъектится в env попытки, локальный скоуп дага
// перекрывает глобальный; отсутствующая переменная валит запуск.
func TestVariables(t *testing.T) {
	e := newEnv(t)
	require.NoError(t, e.variableSvc.Set(context.Background(), "", "api-url", "https://global.example"))

	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "variable-dag",
		"tasks": [{"name": "a", "variables": [{"env": "API_URL", "variable": "api-url"}]}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	assert.Equal(t, "https://global.example", a.Env["API_URL"], "значение переменной в env попытки")
	e.executor.started(a.Ref)
	e.executor.finished(a.Ref, true, 0, "")
	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	// снапшот run_env: переменная — с фактическим значением и скоупом
	env, err := e.runSvc.ListRunEnv(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, env, 1)
	assert.Equal(t, runModel.RunEnvKindVariable, env[0].Kind)
	assert.Equal(t, "https://global.example", env[0].Value)
	assert.Equal(t, "", env[0].Scope)

	// локальная переменная дага перекрывает глобальную
	require.NoError(t, e.variableSvc.Set(context.Background(), dagName, "api-url", "https://local.example"))
	runId2, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return len(e.executor.launchedForRun(runId2)) == 1
	}, waitTimeout, 10*time.Millisecond)
	a2 := e.executor.launchedForRun(runId2)[0]
	assert.Equal(t, "https://local.example", a2.Env["API_URL"])
	e.executor.started(a2.Ref)
	e.executor.finished(a2.Ref, true, 0, "")
	e.waitRunStatus(t, runId2, runModel.RunStatusSuccess)

	// список: значения видны, скоуп различим
	vars, err := e.variableSvc.List(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, vars, 2)

	// отсутствующая переменная валит запуск
	ghostDag := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "ghost-variable-dag",
		"tasks": [{"name": "a", "variables": [{"env": "MISSING", "variable": "ghost"}]}]
	}`)
	ghostRun, err := e.runUsecase.Trigger(context.Background(), ghostDag, nil)
	require.NoError(t, err)

	e.waitRunStatus(t, ghostRun, runModel.RunStatusFailed)
	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), ghostRun)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, "launch_failed", attempts[0].ExitReason)

	// удаление — по скоупу
	require.NoError(t, e.variableSvc.Delete(context.Background(), dagName, "api-url"))
	assert.ErrorIs(t, e.variableSvc.Delete(context.Background(), dagName, "api-url"), errs.VariableNotFound)
}

// Метрики потребления: metrics-события executor'а фиксируют пик памяти
// попытки (greatest — меньший поздний семпл значение не занижает), пик
// переживает финализацию и отдаётся в деталях рана.
func TestAttemptPeakMemory(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "memory-dag",
		"tasks": [{"name": "a"}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	e.executor.started(a.Ref)

	e.executor.metrics(a.Ref, 100<<20)
	e.executor.metrics(a.Ref, 250<<20)
	e.executor.metrics(a.Ref, 200<<20) // меньший семпл пик не занижает

	require.Eventually(t, func() bool {
		attempt, _, gErr := e.runSvc.GetAttempt(context.Background(), a.Ref, true)
		require.NoError(t, gErr)
		return attempt.PeakMemoryBytes != nil && *attempt.PeakMemoryBytes == 250<<20
	}, waitTimeout, 10*time.Millisecond, "пик памяти не зафиксировался")

	e.executor.finished(a.Ref, true, 0, "")
	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	_, _, _, attempts, _, err := e.runSvc.GetDetails(context.Background(), runId)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	require.NotNil(t, attempts[0].PeakMemoryBytes)
	assert.EqualValues(t, 250<<20, *attempts[0].PeakMemoryBytes)
}

// Retention: завершённый ран с истёкшим TTL удаляется целиком — артефакты
// (вызов на artifact-сервер), логи и записи БД; свежие раны не трогаются.
func TestRetentionSweep(t *testing.T) {
	e := newEnv(t)
	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "retention-dag",
		"tasks": [{"name": "a"}]
	}`)

	runId, err := e.runUsecase.Trigger(context.Background(), dagName, nil)
	require.NoError(t, err)

	a := e.waitLaunched(t, "a")
	e.executor.started(a.Ref)
	e.executor.finished(a.Ref, true, 0, "")
	e.waitRunStatus(t, runId, runModel.RunStatusSuccess)

	retention := domainRetention.New(e.runSvc, e.artifact, e.tasklog, nil, time.Hour, time.Hour)

	// ран моложе TTL — не удаляется
	deleted, err := retention.Sweep(context.Background())
	require.NoError(t, err)
	assert.Zero(t, deleted)

	// состариваем ран и чистим
	_, err = e.pool.Exec(context.Background(),
		`UPDATE run SET finished_at = now() - interval '2 hours' WHERE id = $1`, runId)
	require.NoError(t, err)

	deleted, err = retention.Sweep(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	// запись БД удалена (каскадом — таски и попытки)
	_, found, err := e.runSvc.Get(context.Background(), runId, false)
	require.NoError(t, err)
	assert.False(t, found)

	// artifact-сервер получил DeleteRunArtifacts и DeleteRunTaskLogs
	assert.Equal(t, []string{runId}, e.artifact.deleted())
	assert.Equal(t, []string{runId}, e.tasklog.deletedRuns())

	// повторный проход — пусто
	deleted, err = retention.Sweep(context.Background())
	require.NoError(t, err)
	assert.Zero(t, deleted)
}
