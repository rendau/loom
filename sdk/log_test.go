package loom

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Структурный лог таски уходит JSON'ом в sink (лог-стрим, level для
// админки) и text'ом в dup (честный stdout), без дублей между каналами
// (writerSink пропускает source=log).
func TestFanoutLoggerSplitFormats(t *testing.T) {
	var dup bytes.Buffer
	sink := &recordingSink{}

	handlers := []slog.Handler{
		slog.NewJSONHandler(&sinkLineWriter{sink: sink, source: logSourceLog}, nil),
		slog.NewTextHandler(&dup, nil),
	}
	log := slog.New(fanoutHandler{handlers: handlers}).With("dag", "d1", "task", "t1")

	log.Error("task failed", "error", "boom")

	require.Len(t, sink.entries, 1)
	require.Equal(t, logSourceLog, sink.entries[0].source)
	require.True(t, strings.HasPrefix(sink.entries[0].line, `{"`), "sink line must be JSON: %s", sink.entries[0].line)
	require.Contains(t, sink.entries[0].line, `"level":"ERROR"`)
	require.Contains(t, sink.entries[0].line, `"msg":"task failed"`)

	out := dup.String()
	require.Contains(t, out, "level=ERROR")
	require.Contains(t, out, `msg="task failed"`)
	require.NotContains(t, out, `{"`, "stdout must not carry JSON copy")

	// writerSink (fallback без стрима) не дублирует source=log в stdout
	var w bytes.Buffer
	ws := &writerSink{w: &w}
	ws.push(logEntry{source: logSourceLog, line: `{"level":"ERROR"}`})
	ws.push(logEntry{source: logSourceStdout, line: "plain stdout line"})
	require.Equal(t, "plain stdout line\n", w.String())
}

type recordingSink struct {
	entries []logEntry
}

func (s *recordingSink) push(e logEntry) { s.entries = append(s.entries, e) }
func (s *recordingSink) close() error    { return nil }
