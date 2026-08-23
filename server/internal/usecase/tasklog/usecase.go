package tasklog

import (
	"context"
	"fmt"

	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	"github.com/rendau/loom/server/internal/domain/tasklog/model"
)

// Usecase — чтение логов тасков для админки: control plane валидирует
// попытку по БД и проксирует чтение с artifact-сервера, где логи хранятся.
type Usecase struct {
	svc    ServiceI
	runSvc RunServiceI
}

func New(svc ServiceI, runSvc RunServiceI) *Usecase {
	return &Usecase{svc: svc, runSvc: runSvc}
}

// validateAttempt проверяет, что попытка существует, — чтобы не ждать логи
// мусорных ref'ов.
func (u *Usecase) validateAttempt(ctx context.Context, key model.AttemptKey) error {
	ref := runModel.AttemptRef{RunId: key.RunId, Task: key.Task, Attempt: key.Attempt}
	if _, _, err := u.runSvc.GetAttempt(ctx, ref, true); err != nil {
		return fmt.Errorf("runSvc.GetAttempt: %w", err)
	}
	return nil
}

func (u *Usecase) Read(ctx context.Context, key model.AttemptKey, follow bool, fn func([]model.Entry) error) error {
	if err := u.validateAttempt(ctx, key); err != nil {
		return err
	}
	if err := u.svc.ReadTaskLog(ctx, key, follow, fn); err != nil {
		return fmt.Errorf("svc.ReadTaskLog: %w", err)
	}
	return nil
}
