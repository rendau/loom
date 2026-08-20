//go:build !unix

package loom

import (
	"errors"
	"os"
)

// Перехват fd stdout/stderr есть только на unix; контейнеры тасков — linux.
// На прочих платформах RunTask продолжает без перехвата: структурные логи
// доставляются, теряется лишь вывод мимо логгера.
type outputCapture struct {
	Stdout *os.File
}

func newOutputCapture() (*outputCapture, error) {
	return nil, errors.New("output capture is not supported on this platform")
}

func (c *outputCapture) start(logSink) {}

func (c *outputCapture) stop() {}
