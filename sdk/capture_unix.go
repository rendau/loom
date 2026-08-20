//go:build unix

package loom

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// outputCapture перехватывает file descriptor'ы 1 и 2 процесса (dup2):
// паники Go-рантайма и вывод сторонних библиотек уходят мимо логгера —
// прямо в fd. Каждая строка попадает в logSink (source=stdout/stderr),
// а дублирование в настоящий stdout делает сам sink.
//
// Создаётся в два шага: newOutputCapture подменяет fd (после этого Stdout —
// поле структуры, а не fd 1), затем start(sink) запускает читателей пайпов.
// Разрыв нужен, чтобы sink строился уже с настоящим stdout в руках —
// иначе его дублирование зациклится через пайп на него же.
type outputCapture struct {
	Stdout *os.File // dup настоящего fd 1 — сюда sink дублирует строки
	stderr *os.File // dup настоящего fd 2 — для восстановления

	stdoutR, stdoutW *os.File
	stderrR, stderrW *os.File

	wg sync.WaitGroup
}

func newOutputCapture() (c *outputCapture, err error) {
	c = &outputCapture{}
	defer func() {
		if err != nil {
			c.closeFiles()
		}
	}()

	if c.Stdout, err = dupFile(1, "real-stdout"); err != nil {
		return nil, err
	}
	if c.stderr, err = dupFile(2, "real-stderr"); err != nil {
		return nil, err
	}

	if c.stdoutR, c.stdoutW, err = os.Pipe(); err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}
	if c.stderrR, c.stderrW, err = os.Pipe(); err != nil {
		return nil, fmt.Errorf("pipe: %w", err)
	}

	if err = dup2(int(c.stdoutW.Fd()), 1); err != nil {
		return nil, fmt.Errorf("dup2 stdout: %w", err)
	}
	if err = dup2(int(c.stderrW.Fd()), 2); err != nil {
		_ = dup2(int(c.Stdout.Fd()), 1) // откат: вернуть настоящий stdout
		return nil, fmt.Errorf("dup2 stderr: %w", err)
	}

	return c, nil
}

func (c *outputCapture) start(sink logSink) {
	c.wg.Go(func() { scanLines(c.stdoutR, logSourceStdout, sink) })
	c.wg.Go(func() { scanLines(c.stderrR, logSourceStderr, sink) })
}

// stop возвращает fd на место и дочитывает пайпы до конца.
func (c *outputCapture) stop() {
	_ = dup2(int(c.Stdout.Fd()), 1)
	_ = dup2(int(c.stderr.Fd()), 2)

	// закрыть записывающие концы: читатели пайпов получат EOF и завершатся
	_ = c.stdoutW.Close()
	_ = c.stderrW.Close()
	c.wg.Wait()

	_ = c.stdoutR.Close()
	_ = c.stderrR.Close()
	// c.Stdout остаётся открытым: им продолжает пользоваться sink
}

func (c *outputCapture) closeFiles() {
	for _, f := range []*os.File{c.Stdout, c.stderr, c.stdoutR, c.stdoutW, c.stderrR, c.stderrW} {
		if f != nil {
			_ = f.Close()
		}
	}
}

func dupFile(fd int, name string) (*os.File, error) {
	dup, err := unix.Dup(fd)
	if err != nil {
		return nil, fmt.Errorf("dup fd %d: %w", fd, err)
	}
	unix.CloseOnExec(dup)

	return os.NewFile(uintptr(dup), name), nil
}

func scanLines(r io.Reader, source logSource, sink logSink) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			sink.push(logEntry{time: time.Now(), source: source, line: strings.TrimSuffix(line, "\n")})
		}
		if err != nil {
			return
		}
	}
}
