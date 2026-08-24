// Package scheduler — раскрутка графа рана: очередь тасков в Postgres
// (FOR UPDATE SKIP LOCKED), запуск попыток через executor,
// обработка событий их жизненного цикла и финализация (FinishAttempt на
// artifact-сервере, закрытие лог-стрима, каскад по рёбрам графа), ретраи
// с backoff, таймауты тасков и cron-триггер ранов по расписанию дага.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"

	loom "github.com/rendau/loom/sdk"
	"github.com/rendau/loom/server/internal/domain/dag/manifest"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	tasklogModel "github.com/rendau/loom/server/internal/domain/tasklog/model"
	"github.com/rendau/loom/server/internal/util"
)

// finalizeTimeout — на пост-обработку завершения попытки (FinishAttempt на
// artifact-сервере, закрытие лог-стрима) отдельным от событийного цикла
// контекстом.
const finalizeTimeout = 15 * time.Second

// cancelKillConcurrency — сколько попыток останова рана убиваем
// параллельно: docker rm / удаление Job'а идут по одной попытке, а
// остановить ран с десятками живых тасков нужно за один запрос админки.
const cancelKillConcurrency = 8

// errRunNotActive — таск забран из очереди, но его ран уже не выполняется
// (остановлен между claim и launch): запускать нечего.
var errRunNotActive = errors.New("run is not running")

// Политика ретраев: без retry_delay_sec в манифесте пауза перед ретраем —
// defaultRetryDelay; с каждой попыткой пауза удваивается, но не превышает
// maxRetryBackoff.
const (
	defaultRetryDelay = 30 * time.Second
	maxRetryBackoff   = 30 * time.Minute
)

// TaskEnv — адреса data/control plane, какими их видят поды тасков;
// попадают в env-контракт LOOM_* контейнера.
type TaskEnv struct {
	ArtifactAddr string
	ServerAddr   string
}

// Config — параметры планировщика.
type Config struct {
	Tick          time.Duration // период основного прохода (события будят раньше)
	CronTick      time.Duration // период проверки cron-расписаний
	ReconcileTick time.Duration // период зомби-детекта (сверка с executor'ом)
	ZombieGrace   time.Duration // возраст попытки, до которого её не сверяем
	ClaimLimit    int64         // сколько queued-тасков забирать за проход
	TaskEnv       TaskEnv
}

type Scheduler struct {
	runSvc    RunServiceI
	dagSvc    DagServiceI
	executor  ExecutorI
	artifact  ArtifactI
	tasklog   TaskLogI
	secrets   SecretResolverI
	variables VariableResolverI
	cfg       Config

	nudgeCh   chan struct{}
	ctx       context.Context
	ctxCancel context.CancelFunc
	wg        sync.WaitGroup
}

func New(runSvc RunServiceI, dagSvc DagServiceI, executor ExecutorI, artifact ArtifactI, tasklog TaskLogI, secrets SecretResolverI, variables VariableResolverI, cfg Config) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		runSvc:    runSvc,
		dagSvc:    dagSvc,
		executor:  executor,
		artifact:  artifact,
		tasklog:   tasklog,
		secrets:   secrets,
		variables: variables,
		cfg:       cfg,
		nudgeCh:   make(chan struct{}, 1),
		ctx:       ctx,
		ctxCancel: cancel,
	}
}

func (s *Scheduler) Start() {
	s.wg.Go(s.loop)
	s.wg.Go(s.cronLoop)
	s.wg.Go(s.reconcileLoop)
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
	ticker := time.NewTicker(s.cfg.Tick)
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

// cronLoop триггерит раны по расписаниям дагов. Отдельный от pass цикл:
// cron-гранулярность — минуты, будить его чаще tick'а планировщика незачем.
func (s *Scheduler) cronLoop() {
	ticker := time.NewTicker(s.cfg.CronTick)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}

		if err := s.cronPass(s.ctx); err != nil && s.ctx.Err() == nil {
			slog.Error("scheduler cron pass", "error", err)
		}
	}
}

// reconcileLoop — зомби-детект: сверяет незавершённые попытки из БД с
// живыми Job'ами executor'а. Ловит потери событий и рестарты server: попытку,
// зависшую между claim и Launch, и под, умерший без finished-события.
func (s *Scheduler) reconcileLoop() {
	ticker := time.NewTicker(s.cfg.ReconcileTick)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}

		if err := s.reconcilePass(s.ctx); err != nil && s.ctx.Err() == nil {
			slog.Error("scheduler reconcile pass", "error", err)
		}
	}
}

// reconcilePass финализирует зомби-попытки: не начавшие исполняться
// (starting без Job'а — например, server упал между claim и Launch)
// возвращаются в очередь немедленно и без сжигания ретрая; исчезнувшие на
// бегу (running без Job'а) финализируются по обычной политике ретраев.
// Grace-период отсекает гонку с ещё не созданным Launch'ем Job'ом.
func (s *Scheduler) reconcilePass(ctx context.Context) error {
	stale, err := s.runSvc.ListStaleAttempts(ctx, time.Now().Add(-s.cfg.ZombieGrace))
	if err != nil {
		return fmt.Errorf("runSvc.ListStaleAttempts: %w", err)
	}
	if len(stale) == 0 {
		return nil
	}

	aliveRefs, err := s.executor.ListAlive(ctx)
	if err != nil {
		return fmt.Errorf("executor.ListAlive: %w", err)
	}
	alive := lo.SliceToMap(aliveRefs, func(r runModel.AttemptRef) (runModel.AttemptRef, struct{}) {
		return r, struct{}{}
	})

	for _, a := range stale {
		if _, ok := alive[a.Ref]; ok {
			continue
		}

		slog.Warn("zombie attempt detected", "run_id", a.Ref.RunId, "task", a.Ref.Task,
			"attempt", a.Ref.Attempt, "status", a.Status)

		switch a.Status {
		case runModel.AttemptStatusStarting:
			s.finalizeLost(a.Ref, runModel.ExitInfo{
				Success: false,
				Reason:  "lost",
				Message: "attempt lost before start (no job in executor)",
			})
		case runModel.AttemptStatusRunning:
			s.finalize(a.Ref, runModel.ExitInfo{
				Success: false,
				Reason:  "pod_lost",
				Message: "pod disappeared without finish event",
			})
		}
	}

	return nil
}

// catchupMaxPerPass — сколько пропущенных тиков наверстывает catchup-даг за
// один cronPass: троттлинг глубокого бэклога (остаток — следующими проходами).
const catchupMaxPerPass = 10

// cronPass — один проход cron-триггера: даги с наступившим next_run_at.
// Сдвиг next_run_at — compare-and-swap до триггера: при гонке инстансов ран
// создаёт только победитель, а упавший после сдвига триггер теряет тик.
// Обычный даг продолжает расписание от «сейчас» (пропущенные тики
// теряются); catchup-даг двигает next_run_at на следующий тик после
// сработавшего и наверстывает пропущенное.
func (s *Scheduler) cronPass(ctx context.Context) error {
	dags, err := s.dagSvc.ListDueSchedules(ctx)
	if err != nil {
		return fmt.Errorf("dagSvc.ListDueSchedules: %w", err)
	}

	// лаг cron — насколько «сейчас» обогнало самый старый наступивший тик;
	// при пустой выборке расписания не отстают
	lag := time.Duration(0)
	now := time.Now()
	for _, dag := range dags {
		if !dag.NextRunAt.IsZero() {
			lag = max(lag, now.Sub(dag.NextRunAt))
		}
	}
	metricCronLag.Set(lag.Seconds())

	for _, dag := range dags {
		if err = s.cronTriggerDag(ctx, dag); err != nil {
			slog.Error("cron trigger dag", "dag", dag.Name, "error", err)
		}
	}

	return nil
}

func (s *Scheduler) cronTriggerDag(ctx context.Context, dag *dagModel.Main) error {
	now := time.Now()
	tick := dag.NextRunAt

	for range catchupMaxPerPass {
		if tick.After(now) {
			return nil // бэклог исчерпан
		}

		// база следующего тика: у catchup — сработавший тик (наверстываем
		// подряд), у обычного дага — «сейчас» (пропущенное теряется)
		base := now
		if dag.Catchup && !tick.IsZero() {
			base = tick
		}
		next, err := util.CronNext(dag.Schedule, base)
		if err != nil {
			// регистрация валидирует расписание — сюда попадает только
			// legacy-запись; двигать нечего, просто не триггерим
			return fmt.Errorf("cron parse %q: %w", dag.Schedule, err)
		}

		advanced, err := s.dagSvc.AdvanceNextRun(ctx, dag.Name, tick, next)
		if err != nil {
			return fmt.Errorf("advance next run: %w", err)
		}
		if !advanced {
			return nil // другой инстанс успел первым
		}
		if tick.IsZero() {
			return nil // инициализация next_run_at без запуска
		}

		runId, err := s.runSvc.Trigger(ctx, dag, runModel.TriggerSpec{
			Trigger:     runModel.TriggerSchedule,
			LogicalDate: tick, // сработавший тик — «дата данных» рана
		})
		if err != nil {
			return fmt.Errorf("trigger run: %w", err)
		}

		slog.Info("run triggered by schedule", "dag", dag.Name, "run_id", runId,
			"logical_date", tick, "next_run_at", next)
		s.Nudge()

		if !dag.Catchup {
			return nil
		}
		tick = next
	}

	return nil
}

// pass — один проход планировщика: возврат дозревших ретраев в очередь,
// раскрутка графов активных ранов, затем забор готовых тасков из очереди и
// запуск попыток.
func (s *Scheduler) pass(ctx context.Context) error {
	start := time.Now()
	defer func() { metricPassDuration.Observe(time.Since(start).Seconds()) }()
	defer s.sampleQueueMetrics(ctx)

	retried, err := s.runSvc.PromoteRetries(ctx)
	if err != nil {
		return fmt.Errorf("runSvc.PromoteRetries: %w", err)
	}
	if retried > 0 {
		slog.Info("retries promoted to queue", "count", retried)
	}

	runs, err := s.runSvc.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("runSvc.ListActive: %w", err)
	}

	for _, run := range limitActiveRuns(runs) {
		if err = s.replanRun(ctx, run); err != nil {
			slog.Error("scheduler replan run", "run_id", run.Id, "error", err)
		}
	}

	claimed, err := s.runSvc.ClaimQueued(ctx, s.cfg.ClaimLimit)
	if err != nil {
		return fmt.Errorf("runSvc.ClaimQueued: %w", err)
	}

	for _, c := range claimed {
		err = s.launch(ctx, c)
		if err == nil {
			continue
		}

		ref := runModel.AttemptRef{RunId: c.RunId, Task: c.Task, Attempt: c.Attempt}

		// ран остановили между claim и launch — попытку не запускаем, а сразу
		// закрываем как остановленную (иначе таск завис бы в starting)
		if errors.Is(err, errRunNotActive) {
			slog.Info("claimed task dropped: run is not running",
				"run_id", c.RunId, "task", c.Task, "attempt", c.Attempt)
			s.finalizeCanceled(ref)
			continue
		}

		slog.Error("scheduler launch", "run_id", c.RunId, "task", c.Task, "attempt", c.Attempt, "error", err)
		s.finalize(ref, runModel.ExitInfo{
			Success: false,
			Reason:  "launch_failed",
			Message: err.Error(),
		})
	}

	// запуски перевели таски в starting — стримовые потомки могли стать
	// готовыми; дорасткрутит следующий проход (Nudge из finalize/launch не
	// нужен: очередной тик и события executor'а покрывают это)
	return nil
}

// sampleQueueMetrics обновляет гейджи глубины очереди и занятости пулов;
// метрики best-effort — ошибки не валят проход.
func (s *Scheduler) sampleQueueMetrics(ctx context.Context) {
	counts, err := s.runSvc.CountActiveTaskInstances(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("sample task instance metrics", "error", err)
		}
		return
	}
	for _, status := range []string{runModel.TaskStatusPending, runModel.TaskStatusQueued,
		runModel.TaskStatusStarting, runModel.TaskStatusRunning, runModel.TaskStatusUpForRetry} {
		metricTaskInstances.WithLabelValues(status).Set(float64(counts[status]))
	}

	pools, err := s.runSvc.ListPoolUsage(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("sample pool metrics", "error", err)
		}
		return
	}
	for _, p := range pools {
		metricPoolSlots.WithLabelValues(p.Pool).Set(float64(p.Slots))
		metricPoolBusy.WithLabelValues(p.Pool).Set(float64(p.Busy))
	}
}

// limitActiveRuns применяет max_active_runs: у дага с лимитом
// раскручиваются только N старейших активных ранов, у остальных таски
// остаются pending до освобождения места. Лимит берётся из манифеста самого
// свежего рана дага (актуальная регистрация); финализация запущенных попыток
// событийная и от replan не зависит — пропуск рана безопасен.
func limitActiveRuns(runs []*runModel.Main) []*runModel.Main {
	sorted := make([]*runModel.Main, len(runs))
	copy(sorted, runs)
	slices.SortStableFunc(sorted, func(a, b *runModel.Main) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	limits := map[string]int{}
	for _, run := range sorted { // после цикла — значение самого свежего рана
		m, err := manifest.Parse(run.Manifest)
		if err != nil {
			continue
		}
		limits[run.DagName] = m.MaxActiveRuns
	}

	taken := map[string]int{}
	result := make([]*runModel.Main, 0, len(sorted))
	for _, run := range sorted {
		limit := limits[run.DagName]
		if limit > 0 && taken[run.DagName] >= limit {
			continue
		}
		taken[run.DagName]++
		result = append(result, run)
	}
	return result
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

	// сторожевой таймаут: зависшие попытки убиваем и финализируем с
	// reason=timeout (ретраи — по обычной политике); план перестраиваем уже
	// на следующем проходе, по свежим статусам
	if s.killTimedOut(ctx, run.Id, m.Tasks, tis) > 0 {
		return nil
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
		applied, err := s.runSvc.FinishRun(ctx, run.Id, p.RunStatus)
		if err != nil {
			return fmt.Errorf("runSvc.FinishRun: %w", err)
		}
		if applied {
			metricRunFinished.WithLabelValues(p.RunStatus).Inc()
			metricRunDuration.WithLabelValues(p.RunStatus).Observe(time.Since(run.CreatedAt).Seconds())
		}
		slog.Info("run finished", "run_id", run.Id, "status", p.RunStatus)
	}

	return nil
}

// killTimedOut находит running-попытки, превысившие timeout_sec манифеста,
// убивает их через executor и финализирует. Возвращает число убитых.
func (s *Scheduler) killTimedOut(ctx context.Context, runId string, tasks []dagModel.Task, tis []*runModel.TaskInstance) int {
	timeouts := lo.SliceToMap(tasks, func(t dagModel.Task) (string, int) {
		return t.Name, t.TimeoutSec
	})

	killed := 0
	now := time.Now()
	for _, ti := range tis {
		sec := timeouts[ti.Task]
		if ti.Status != runModel.TaskStatusRunning || sec <= 0 || ti.StartedAt.IsZero() {
			continue
		}
		if now.Before(ti.StartedAt.Add(time.Duration(sec) * time.Second)) {
			continue
		}

		ref := runModel.AttemptRef{RunId: runId, Task: ti.Task, Attempt: ti.Attempt}
		slog.Warn("attempt timed out, killing", "run_id", runId, "task", ti.Task, "attempt", ti.Attempt, "timeout_sec", sec)

		if err := s.executor.Kill(ctx, ref); err != nil {
			// не смогли убить — всё равно финализируем: запоздалое событие
			// умершего пода схлопнется идемпотентной финализацией
			slog.Warn("executor kill", "run_id", runId, "task", ti.Task, "attempt", ti.Attempt, "error", err)
		}

		s.finalize(ref, runModel.ExitInfo{
			Success: false,
			Reason:  "timeout",
			Message: fmt.Sprintf("task timed out after %ds", sec),
		})
		killed++
	}

	return killed
}

// launch запускает попытку забранного из очереди таска: собирает env-контракт
// LOOM_* (включая номера попыток зависимостей) и отдаёт executor'у.
func (s *Scheduler) launch(ctx context.Context, c runModel.ClaimedTask) error {
	run, _, err := s.runSvc.Get(ctx, c.RunId, true)
	if err != nil {
		return fmt.Errorf("runSvc.Get: %w", err)
	}

	if run.Status != runModel.RunStatusRunning {
		return errRunNotActive
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
		Ref:     runModel.AttemptRef{RunId: c.RunId, Task: c.Task, Attempt: c.Attempt},
		DagName: run.DagName,
		Image:   run.ImageDigest,
		Env: map[string]string{
			loom.EnvRunID:        c.RunId,
			loom.EnvTask:         c.Task,
			loom.EnvAttempt:      strconv.Itoa(int(c.Attempt)),
			loom.EnvDepAttempts:  string(depAttemptsJson),
			loom.EnvArtifactAddr: s.cfg.TaskEnv.ArtifactAddr,
			loom.EnvServerAddr:   s.cfg.TaskEnv.ServerAddr,
			loom.EnvLogicalDate:  run.LogicalDate.UTC().Format(time.RFC3339),
		},
		Resources: task.Resources,
	}
	if len(run.Params) > 0 {
		spec.Env[loom.EnvRunParams] = string(run.Params)
	}

	// секреты и переменные манифеста → env контейнера (локальный скоуп дага
	// перекрывает глобальный); отсутствующее имя валит запуск (launch_failed),
	// после добавления попытку вернёт RetryTask
	if len(task.Secrets) > 0 {
		names := lo.Uniq(lo.Map(task.Secrets, func(s dagModel.SecretRef, _ int) string { return s.Secret }))
		values, err := s.secrets.ResolveValues(ctx, run.DagName, names)
		if err != nil {
			return fmt.Errorf("resolve secrets: %w", err)
		}
		for _, sec := range task.Secrets {
			spec.Env[sec.Env] = string(values[sec.Secret])
		}
	}
	if len(task.Variables) > 0 {
		names := lo.Uniq(lo.Map(task.Variables, func(v dagModel.VariableRef, _ int) string { return v.Variable }))
		values, err := s.variables.ResolveValues(ctx, run.DagName, names)
		if err != nil {
			return fmt.Errorf("resolve variables: %w", err)
		}
		for _, v := range task.Variables {
			spec.Env[v.Env] = values[v.Variable]
		}
	}

	if err = s.executor.Launch(ctx, spec); err != nil {
		metricLaunchErrors.Inc()
		return fmt.Errorf("executor.Launch: %w", err)
	}
	metricLaunches.Inc()

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

	case runModel.ExecEventMetrics:
		if ev.PeakMemoryBytes == nil {
			return
		}
		// без Nudge: метрика не меняет план
		if err := s.runSvc.SetAttemptPeakMemory(s.ctx, ev.Ref, *ev.PeakMemoryBytes); err != nil && s.ctx.Err() == nil {
			slog.Error("set attempt peak memory", "run_id", ev.Ref.RunId, "task", ev.Ref.Task,
				"attempt", ev.Ref.Attempt, "error", err)
		}
	}
}

// finalize фиксирует завершение попытки: статусы в БД (при остатке ретраев —
// up_for_retry с backoff), страховочный FinishAttempt на artifact-сервере,
// дописывание exit-информации в лог и его commit. Идемпотентен — дубли
// событий и страховочные вызовы схлопываются на FinalizeAttempt.
func (s *Scheduler) finalize(ref runModel.AttemptRef, exit runModel.ExitInfo) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), finalizeTimeout)
	defer cancel()

	s.finalizeCtx(ctx, ref, exit, s.retryAt(ctx, ref, exit))
}

// CancelAttempts убивает и финализирует живые попытки остановленного рана
// (сам ран и его нестартовавшие таски перевёл в canceled usecase). Kill
// best-effort: не убитая попытка всё равно финализируется, а её запоздалое
// событие схлопнется идемпотентной финализацией.
func (s *Scheduler) CancelAttempts(ctx context.Context, refs []runModel.AttemptRef) {
	if len(refs) == 0 {
		return
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(cancelKillConcurrency)

	for _, ref := range refs {
		eg.Go(func() error {
			if err := s.executor.Kill(egCtx, ref); err != nil {
				slog.Warn("executor kill on cancel", "run_id", ref.RunId, "task", ref.Task,
					"attempt", ref.Attempt, "error", err)
			}
			s.finalizeCanceled(ref)
			return nil
		})
	}
	_ = eg.Wait() // задачи не возвращают ошибок: errgroup здесь — лимит параллелизма
}

// finalizeCanceled закрывает попытку остановленного рана: ретрай не
// назначается (ран терминален), таск получает canceled.
func (s *Scheduler) finalizeCanceled(ref runModel.AttemptRef) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), finalizeTimeout)
	defer cancel()

	s.finalizeCtx(ctx, ref, runModel.ExitInfo{
		Reason:   "canceled",
		Message:  "ран остановлен вручную",
		Canceled: true,
	}, nil)
}

// finalizeLost финализирует попытку, потерянную до старта: немедленный
// возврат в очередь вне политики ретраев — таск не исполнялся, сжигать его
// ретрай за сбой инфраструктуры нечестно.
func (s *Scheduler) finalizeLost(ref runModel.AttemptRef, exit runModel.ExitInfo) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), finalizeTimeout)
	defer cancel()

	s.finalizeCtx(ctx, ref, exit, new(time.Now()))
}

func (s *Scheduler) finalizeCtx(ctx context.Context, ref runModel.AttemptRef, exit runModel.ExitInfo, retryAt *time.Time) {
	applied, startedAt, err := s.runSvc.FinalizeAttempt(ctx, ref, exit, retryAt)
	if err != nil {
		slog.Error("finalize attempt", "run_id", ref.RunId, "task", ref.Task, "attempt", ref.Attempt, "error", err)
		return
	}
	if !applied {
		return
	}

	metricAttemptFinished.WithLabelValues(strconv.FormatBool(exit.Success), exit.Reason).Inc()
	if startedAt != nil {
		metricAttemptDuration.WithLabelValues(strconv.FormatBool(exit.Success)).Observe(time.Since(*startedAt).Seconds())
	}

	// страховка: SDK вызывает FinishAttempt сам, но при смерти пода вызова
	// могло не быть — повторяем, вызов идемпотентен
	if err = s.artifact.FinishAttempt(ctx, ref); err != nil {
		slog.Warn("artifact finish attempt", "run_id", ref.RunId, "task", ref.Task, "attempt", ref.Attempt, "error", err)
	}

	key := tasklogModel.AttemptKey{RunId: ref.RunId, Task: ref.Task, Attempt: ref.Attempt}
	if err = s.tasklog.Finish(ctx, key, []tasklogModel.Entry{exitLogEntry(exit, retryAt)}); err != nil {
		slog.Warn("finish task log", "run_id", ref.RunId, "task", ref.Task, "attempt", ref.Attempt, "error", err)
	}

	slog.Info("attempt finished", "run_id", ref.RunId, "task", ref.Task, "attempt", ref.Attempt,
		"success", exit.Success, "reason", exit.Reason, "retry", retryAt != nil)

	s.Nudge()
}

// retryAt решает, ретраить ли неуспешную попытку, по политике таска из
// манифеста рана: nil — ретраев не осталось (или попытка успешна). Если
// манифест недоступен, деградируем к «без ретрая» — финализация важнее.
func (s *Scheduler) retryAt(ctx context.Context, ref runModel.AttemptRef, exit runModel.ExitInfo) *time.Time {
	if exit.Success {
		return nil
	}

	run, _, err := s.runSvc.Get(ctx, ref.RunId, true)
	if err != nil {
		slog.Warn("retry policy lookup", "run_id", ref.RunId, "task", ref.Task, "error", err)
		return nil
	}

	m, err := manifest.Parse(run.Manifest)
	if err != nil {
		slog.Warn("retry policy manifest parse", "run_id", ref.RunId, "error", err)
		return nil
	}

	task, ok := lo.Find(m.Tasks, func(t dagModel.Task) bool { return t.Name == ref.Task })
	if !ok || ref.Attempt > int32(task.Retries) {
		return nil // попытки исчерпаны: retries+1 всего
	}

	return new(time.Now().Add(retryBackoff(task.RetryDelaySec, ref.Attempt)))
}

// retryBackoff — пауза перед следующей попыткой: базовая retry_delay_sec
// (или defaultRetryDelay), удвоение с каждой неуспешной попыткой, потолок
// maxRetryBackoff.
func retryBackoff(delaySec int, failedAttempt int32) time.Duration {
	delay := time.Duration(delaySec) * time.Second
	if delay <= 0 {
		delay = defaultRetryDelay
	}

	for i := int32(1); i < failedAttempt && delay < maxRetryBackoff; i++ {
		delay *= 2
	}

	return min(delay, maxRetryBackoff)
}

// exitLogEntry — строка от control plane с исходом попытки; при смерти SDK
// вместе с процессом это единственный след причины смерти в логе.
func exitLogEntry(exit runModel.ExitInfo, retryAt *time.Time) tasklogModel.Entry {
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
	if retryAt != nil {
		line += ", retry scheduled at " + retryAt.UTC().Format(time.RFC3339)
	}

	return tasklogModel.Entry{
		TsUnixMs: time.Now().UnixMilli(),
		Source:   tasklogModel.SourceServer,
		Line:     line,
	}
}
