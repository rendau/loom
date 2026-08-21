package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	json "github.com/goccy/go-json"
)

// Env-контракт контейнера таска: executor передаёт параметры запуска через
// переменные окружения (значения флагов run --task имеют приоритет).
const (
	EnvServerAddr   = "LOOM_SERVER_ADDR"   // адрес control plane (лог-стрим); пусто — логи только в stdout
	EnvArtifactAddr = "LOOM_ARTIFACT_ADDR" // адрес artifact-сервера (data plane)
	EnvRunID        = "LOOM_RUN_ID"
	EnvTask         = "LOOM_TASK"
	EnvAttempt      = "LOOM_ATTEMPT"
	EnvDepAttempts  = "LOOM_DEP_ATTEMPTS" // json-объект {"task": attempt}: чьи артефакты читать
	EnvToken        = "LOOM_TOKEN"        // короткоживущий токен, скоупленный на attempt
)

// finishAttemptTimeout — на FinishAttempt после завершения таска; вызов
// идёт отдельным контекстом: даже отменённый (SIGTERM) таск обязан
// разблокировать читателей своих несозданных артефактов.
const finishAttemptTimeout = 10 * time.Second

// TaskRunSpec — параметры запуска одного таска в распределённом режиме.
// Обычно собирается из env-контракта (константы Env*) командой
// `<dag-binary> run --task=...`; напрямую RunTask зовут интеграционные
// тесты и кастомные раннеры.
type TaskRunSpec struct {
	RunID   string
	Task    string
	Attempt int

	ArtifactAddr string         // адрес artifact-сервера, обязателен
	ServerAddr   string         // адрес control plane; пусто — без лог-стрима
	Token        string         // attempt-токен; прикладывается metadata'ой к вызовам
	DepAttempts  map[string]int // таск-зависимость → номер попытки (по умолчанию 1)

	CaptureOutput bool // перехват fd stdout/stderr (dup2) в лог-стрим
}

func (s TaskRunSpec) validate() error {
	var errs []error
	if s.Task == "" {
		errs = append(errs, errors.New("task is required"))
	}
	if s.RunID == "" {
		errs = append(errs, errors.New("run id is required"))
	}
	if s.Attempt < 1 {
		errs = append(errs, fmt.Errorf("invalid attempt %d", s.Attempt))
	}
	if s.ArtifactAddr == "" {
		errs = append(errs, errors.New("artifact server address is required"))
	}

	return errors.Join(errs...)
}

// taskRunSpecFromEnv читает env-контракт executor'а.
func taskRunSpecFromEnv() (TaskRunSpec, error) {
	spec := TaskRunSpec{
		RunID:        os.Getenv(EnvRunID),
		Task:         os.Getenv(EnvTask),
		ArtifactAddr: os.Getenv(EnvArtifactAddr),
		ServerAddr:   os.Getenv(EnvServerAddr),
		Token:        os.Getenv(EnvToken),
	}

	if raw := os.Getenv(EnvAttempt); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return spec, fmt.Errorf("parse %s: %w", EnvAttempt, err)
		}
		spec.Attempt = n
	}

	if raw := os.Getenv(EnvDepAttempts); raw != "" {
		if err := json.Unmarshal([]byte(raw), &spec.DepAttempts); err != nil {
			return spec, fmt.Errorf("parse %s: %w", EnvDepAttempts, err)
		}
	}

	return spec, nil
}

// RunTask выполняет один таск дага в распределённом режиме: артефакты — через
// artifact-сервер, логи — стримом на control plane (и дублем в stdout).
// По завершении попытки — независимо от исхода — вызывает FinishAttempt:
// остатки записей abort'ятся, читатели несозданных артефактов получают
// NOT_FOUND. Ошибка таска (включая панику) — в возвращаемом значении.
func (d *DAG) RunTask(ctx context.Context, spec TaskRunSpec) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := spec.validate(); err != nil {
		return err
	}

	t, ok := d.tasks[spec.Task]
	if !ok {
		return fmt.Errorf("unknown task %q", spec.Task)
	}

	// настоящий stdout фиксируем до подмены fd: sink дублирует туда каждую
	// строку — это страховка на случай смерти SDK вместе с процессом
	dup := io.Writer(os.Stdout)
	var capture *outputCapture
	if spec.CaptureOutput {
		var err error
		if capture, err = newOutputCapture(); err != nil {
			fmt.Fprintf(os.Stderr, "loom: output capture disabled: %v\n", err)
			capture = nil
		} else {
			dup = capture.Stdout
		}
	}

	sink := newLogSink(spec, dup)
	if capture != nil {
		capture.start(sink)
	}

	err := d.runTaskWithSink(ctx, t, spec, sink)

	// порядок: сначала дочитать пайпы перехвата в sink, затем дослать sink
	if capture != nil {
		capture.stop()
	}
	if closeErr := sink.close(); closeErr != nil {
		fmt.Fprintf(dup, "loom: task log stream: %v\n", closeErr)
	}

	return err
}

// newLogSink собирает sink по спеке: gRPC-стрим на control plane или, без
// него (нет адреса / стрим не открылся), только дубль в настоящий stdout.
func newLogSink(spec TaskRunSpec, dup io.Writer) logSink {
	if spec.ServerAddr == "" {
		return &writerSink{w: dup}
	}

	sink, err := newGrpcLogSink(spec.ServerAddr, spec.Token, dup, spec.RunID, spec.Task, spec.Attempt)
	if err != nil {
		fmt.Fprintf(dup, "loom: task log stream disabled: %v\n", err)
		return &writerSink{w: dup}
	}

	return sink
}

func (d *DAG) runTaskWithSink(ctx context.Context, t *Task, spec TaskRunSpec, sink logSink) error {
	store, err := dialGrpcStore(spec.ArtifactAddr, spec.Token)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	log := slog.New(slog.NewTextHandler(&sinkLineWriter{sink: sink, source: logSourceLog}, nil)).
		With("dag", d.name, "run_id", spec.RunID, "task", spec.Task, "attempt", spec.Attempt)

	// таймаут таска: дедлайн видят и тело таска, и операции Runtime
	// (блокирующее follow-чтение артефактов); страховочный kill зависшей
	// попытки — на control plane
	taskCtx, taskCancel := taskContext(ctx, t)
	defer taskCancel()

	rt := newRuntime(taskCtx, t, spec.RunID, spec.Attempt, log, store, spec.DepAttempts)

	log.Info("task started")
	startedAt := time.Now()

	taskErr := runTaskFn(taskCtx, t, rt)
	finErr := rt.finish(taskErr)

	faCtx, faCancel := context.WithTimeout(context.WithoutCancel(ctx), finishAttemptTimeout)
	faErr := store.finishAttempt(faCtx, spec.RunID, spec.Task, spec.Attempt)
	faCancel()

	err = taskErr
	if err == nil {
		err = errors.Join(finErr, faErr)
	}

	if err != nil {
		log.Error("task failed", "error", err, "duration", time.Since(startedAt))
		err = fmt.Errorf("task %q: %w", t.name, err)
	} else {
		log.Info("task succeeded", "duration", time.Since(startedAt))
	}

	return err
}
