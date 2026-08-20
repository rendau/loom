package loom

import (
	"bytes"
	"fmt"
	"io"
	"time"
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
// шлёт логи на control plane и дублирует их в честный stdout контейнера.
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
	fmt.Fprintln(s.w, e.line)
}

func (s *writerSink) close() error {
	return nil
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
