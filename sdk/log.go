package loom

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/samber/lo"
)

// logSource — происхождение строки лога таска: структурный логгер Runtime
// или перехваченные file descriptor'ы процесса (stdout/stderr).
type logSource string

const (
	logSourceLog    logSource = "log"
	logSourceStdout logSource = "stdout"
	logSourceStderr logSource = "stderr"
)

// logEntry — строка лога таска.
type logEntry struct {
	time   time.Time
	source logSource
	line   string
}

// logSink — порт доставки логов таска. push не ходит в сеть синхронно:
// батчинг и отправка — забота реализации; close дожидается доставки буферов.
// Локальный режим обходится без sink (slog прямо в stderr), распределённый
// шлёт логи на control plane; в честный stdout контейнера sink дублирует
// только строки перехвата stdout/stderr — text-копию структурных строк
// логгера туда пишет второй хендлер fanout-логгера (см. runTaskWithSink).
type logSink interface {
	push(e logEntry)
	close() error
}

// writerSink — logSink без сети: строки уходят только в w (настоящий
// stdout). Используется, когда control plane не задан (нет LOOM_SERVER_ADDR),
// и как fallback при недоступном лог-стриме.
type writerSink struct {
	w io.Writer
}

func (s *writerSink) push(e logEntry) {
	// структурные строки логгера в stdout не дублируем: их текстовую копию
	// туда уже пишет второй хендлер fanout-логгера (см. runTaskWithSink)
	if e.source == logSourceLog {
		return
	}
	fmt.Fprintln(s.w, e.line)
}

func (s *writerSink) close() error {
	return nil
}

// fanoutHandler раздаёт каждую slog-запись всем хендлерам. Нужен, чтобы
// структурный лог таски шёл двумя форматами по назначению: JSON — в
// лог-стрим artifact-сервера (админка фильтрует по level), text — в честный
// stdout контейнера (читаемо в kubectl logs, и сборщик логов кластера не
// принимает рабочие error-строки тасков за алёрты сервиса).
type fanoutHandler struct {
	handlers []slog.Handler
}

func (h fanoutHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return lo.SomeBy(h.handlers, func(hh slog.Handler) bool { return hh.Enabled(ctx, lvl) })
}

func (h fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var errs []error
	for _, hh := range h.handlers {
		if hh.Enabled(ctx, r.Level) {
			errs = append(errs, hh.Handle(ctx, r.Clone()))
		}
	}

	return errors.Join(errs...)
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return fanoutHandler{handlers: lo.Map(h.handlers, func(hh slog.Handler, _ int) slog.Handler {
		return hh.WithAttrs(attrs)
	})}
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	return fanoutHandler{handlers: lo.Map(h.handlers, func(hh slog.Handler, _ int) slog.Handler {
		return hh.WithGroup(name)
	})}
}

// sinkLineWriter адаптирует logSink к io.Writer для slog-хендлера: каждая
// запись логгера приходит одним Write со строкой, завершённой '\n'.
type sinkLineWriter struct {
	sink   logSink
	source logSource
}

func (w *sinkLineWriter) Write(p []byte) (int, error) {
	for line := range bytes.Lines(p) {
		w.sink.push(logEntry{
			time:   time.Now(),
			source: w.source,
			line:   string(bytes.TrimSuffix(line, []byte("\n"))),
		})
	}

	return len(p), nil
}
