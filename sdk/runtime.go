package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/samber/lo"
)

// ArtifactRef адресует артефакт. Артефакты скоупятся на попытку таска:
// ретрай пишет свои артефакты заново, не трогая данные прошлых попыток.
type ArtifactRef struct {
	RunID   string
	Task    string
	Attempt int
	Name    string
}

// artifactStore — порт обмена данными между тасками. Реализации:
// in-memory (локальный режим) и grpc-клиент artifact-сервера (распределённый).
type artifactStore interface {
	OpenWrite(ctx context.Context, ref ArtifactRef) (ArtifactWriter, error)
	// OpenRead открывает чтение с follow-семантикой: читатель, догнавший
	// хвост пишущегося артефакта, ждёт новых данных; io.EOF — только после
	// commit записи, abort записи возвращает ошибку.
	OpenRead(ctx context.Context, ref ArtifactRef) (io.ReadCloser, error)
}

// ArtifactWriter — запись артефакта. Commit делает данные видимыми целиком,
// Abort инвалидирует поток: читатели получат ошибку.
type ArtifactWriter interface {
	io.Writer
	Commit() error
	Abort() error
}

// Runtime — единственная точка контакта таска с внешним миром:
// артефакты и логи.
type Runtime struct {
	ctx         context.Context
	task        *Task
	runID       string
	attempt     int
	log         *slog.Logger
	store       artifactStore
	depAttempts map[string]int

	mu      sync.Mutex
	writers map[string]*outputWriter
}

func newRuntime(ctx context.Context, task *Task, runID string, attempt int, log *slog.Logger, store artifactStore, depAttempts map[string]int) *Runtime {
	return &Runtime{
		ctx:         ctx,
		task:        task,
		runID:       runID,
		attempt:     attempt,
		log:         log,
		store:       store,
		depAttempts: depAttempts,
		writers:     map[string]*outputWriter{},
	}
}

func (rt *Runtime) Log() *slog.Logger {
	return rt.log
}

// Output открывает артефакт на запись. Close опционален: он лишь запрещает
// дальнейшую запись в этот выход. Commit всех выходов происходит при
// успешном завершении таска, abort — при его ошибке или панике: артефакт
// становится валидным для читателей только вместе с успехом попытки.
func (rt *Runtime) Output(name string) (io.WriteCloser, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid artifact name %q", name)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if _, ok := rt.writers[name]; ok {
		return nil, fmt.Errorf("output %q is already open", name)
	}

	w, err := rt.store.OpenWrite(rt.ctx, ArtifactRef{RunID: rt.runID, Task: rt.task.name, Attempt: rt.attempt, Name: name})
	if err != nil {
		return nil, fmt.Errorf("open output %q: %w", name, err)
	}

	ow := &outputWriter{w: w}
	rt.writers[name] = ow

	return ow, nil
}

// Input открывает артефакт таска-зависимости на чтение. Читать можно только
// у тасков, объявленных зависимостями через After/AfterStreamed — так граф
// данных остаётся честным подмножеством графа управления.
func (rt *Runtime) Input(task, name string) (io.ReadCloser, error) {
	if !lo.ContainsBy(rt.task.deps, func(v taskDep) bool { return v.task.name == task }) {
		return nil, fmt.Errorf("task %q is not a declared dependency of %q", task, rt.task.name)
	}

	r, err := rt.store.OpenRead(rt.ctx, ArtifactRef{RunID: rt.runID, Task: task, Attempt: rt.depAttempt(task), Name: name})
	if err != nil {
		return nil, fmt.Errorf("open input %q/%q: %w", task, name, err)
	}

	return r, nil
}

// depAttempt возвращает номер попытки зависимости, чьи артефакты читаем.
// В локальном режиме попытка всегда 1; в распределённом номера назначает
// control plane (env-контракт LOOM_DEP_ATTEMPTS).
func (rt *Runtime) depAttempt(task string) int {
	if n, ok := rt.depAttempts[task]; ok {
		return n
	}

	return 1
}

// finish завершает все выходы по результату таска: commit при успехе,
// abort при ошибке. На программиста здесь не полагаемся — забытый Close
// ничего не ломает, а ранний Close ничего не коммитит.
func (rt *Runtime) finish(taskErr error) error {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	var errs []error
	for name, w := range rt.writers {
		if taskErr != nil {
			if err := w.abort(); err != nil {
				errs = append(errs, fmt.Errorf("abort output %q: %w", name, err))
			}
		} else if err := w.commit(); err != nil {
			errs = append(errs, fmt.Errorf("commit output %q: %w", name, err))
		}
	}

	return errors.Join(errs...)
}

// outputWriter — обёртка над ArtifactWriter. Close пользователя лишь
// запрещает дальнейшую запись; commit/abort делает только Runtime по
// результату таска, ровно один раз.
type outputWriter struct {
	mu       sync.Mutex
	w        ArtifactWriter
	closed   bool // пользователь закрыл выход — запись запрещена
	finished bool // Runtime уже закоммитил или abort'нул
}

func (w *outputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed || w.finished {
		return 0, errors.New("write to closed output")
	}

	return w.w.Write(p)
}

func (w *outputWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.closed = true

	return nil
}

func (w *outputWriter) commit() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.finished {
		return nil
	}
	w.finished = true

	return w.w.Commit()
}

func (w *outputWriter) abort() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.finished {
		return nil
	}
	w.finished = true

	return w.w.Abort()
}
