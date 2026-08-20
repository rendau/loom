//go:build unix

package loom

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

type recordSink struct {
	mu      sync.Mutex
	entries []logEntry
}

func (s *recordSink) push(e logEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = append(s.entries, e)
}

func (s *recordSink) close() error {
	return nil
}

func (s *recordSink) lines(source logSource) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return lo.FilterMap(s.entries, func(e logEntry, _ int) (string, bool) {
		return e.line, e.source == source
	})
}

func TestCaptureOutput(t *testing.T) {
	sink := &recordSink{}

	capture, err := newOutputCapture()
	require.NoError(t, err)
	capture.start(sink)

	// печать мимо логгера — в перехваченные fd 1 и 2
	fmt.Println("captured-stdout-line")
	fmt.Fprintln(os.Stderr, "captured-stderr-line")

	capture.stop()

	require.Equal(t, []string{"captured-stdout-line"}, sink.lines(logSourceStdout))
	require.Equal(t, []string{"captured-stderr-line"}, sink.lines(logSourceStderr))
}
