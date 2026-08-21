package loom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	json "github.com/goccy/go-json"
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
// артефакты, параметры рана и логи.
type Runtime struct {
	ctx         context.Context
	task        *Task
	runID       string
	attempt     int
	log         *slog.Logger
	store       artifactStore
	values      valueStore
	depAttempts map[string]int
	params      []byte
	logicalDate time.Time

	mu      sync.Mutex
	writers map[string]*outputWriter
}

// runtimeCfg — параметры создания Runtime (общие для локального и
// распределённого режимов).
type runtimeCfg struct {
	runID       string
	attempt     int
	store       artifactStore
	values      valueStore // nil — значения недоступны (нет control plane)
	depAttempts map[string]int
	params      []byte
	logicalDate time.Time
}

func newRuntime(ctx context.Context, task *Task, log *slog.Logger, cfg runtimeCfg) *Runtime {
	return &Runtime{
		ctx:         ctx,
		task:        task,
		runID:       cfg.runID,
		attempt:     cfg.attempt,
		log:         log,
		store:       cfg.store,
		values:      cfg.values,
		depAttempts: cfg.depAttempts,
		params:      cfg.params,
		logicalDate: cfg.logicalDate,
		writers:     map[string]*outputWriter{},
	}
}

func (rt *Runtime) Log() *slog.Logger {
	return rt.log
}

// Params возвращает параметры рана как raw JSON-объект; nil — ран без
// параметров. Для типизированного доступа — BindParams.
func (rt *Runtime) Params() []byte {
	return rt.params
}

// BindParams десериализует параметры рана в v; ран без параметров — ошибка
// (проверяйте Params(), если параметры опциональны).
func (rt *Runtime) BindParams(v any) error {
	if len(rt.params) == 0 {
		return errors.New("run has no params")
	}
	if err := json.Unmarshal(rt.params, v); err != nil {
		return fmt.Errorf("unmarshal run params: %w", err)
	}
	return nil
}

// LogicalDate — «дата данных» рана: тик расписания у cron/backfill-рана,
// момент триггера у ручного и локального.
func (rt *Runtime) LogicalDate() time.Time {
	return rt.logicalDate
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

// PushValue публикует мелкое значение таска (аналог XCom): счётчики, id,
// статусы — не данные (данные текут артефактами, Output). v сериализуется в
// JSON, лимит — 64KB; повторный пуш по тому же ключу перезаписывает.
// В распределённом режиме требует control plane (LOOM_SERVER_ADDR).
func (rt *Runtime) PushValue(key string, v any) error {
	if rt.values == nil {
		return errors.New("task values require control plane (LOOM_SERVER_ADDR is not set)")
	}
	if !nameRe.MatchString(key) {
		return fmt.Errorf("invalid value key %q", key)
	}

	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal value %q: %w", key, err)
	}

	if err = rt.values.Push(rt.ctx, rt.runID, rt.task.name, rt.attempt, key, raw); err != nil {
		return fmt.Errorf("push value %q: %w", key, err)
	}
	return nil
}

// PullValue читает значение таска-зависимости в dest. Как и Input, доступны
// только таски, объявленные через After/AfterStreamed. Отсутствующее
// значение — ошибка (у стримовой зависимости оно может ещё не появиться).
func (rt *Runtime) PullValue(task, key string, dest any) error {
	if rt.values == nil {
		return errors.New("task values require control plane (LOOM_SERVER_ADDR is not set)")
	}
	if !lo.ContainsBy(rt.task.deps, func(v taskDep) bool { return v.task.name == task }) {
		return fmt.Errorf("task %q is not a declared dependency of %q", task, rt.task.name)
	}

	raw, err := rt.values.Pull(rt.ctx, rt.runID, task, key)
	if err != nil {
		return fmt.Errorf("pull value %q/%q: %w", task, key, err)
	}

	if err = json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("unmarshal value %q/%q: %w", task, key, err)
	}
	return nil
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
