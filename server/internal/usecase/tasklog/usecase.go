package tasklog

import (
	"context"
	"fmt"

	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	"github.com/rendau/loom/server/internal/domain/tasklog/model"
)

type Usecase struct {
	svc    ServiceI
	runSvc RunServiceI
}

func New(svc ServiceI, runSvc RunServiceI) *Usecase {
	return &Usecase{svc: svc, runSvc: runSvc}
}

// ValidateAttempt проверяет, что попытка существует, — вызывается на header
// push-стрима и перед чтением, чтобы не писать/не ждать логи мусорных ref'ов.
func (u *Usecase) ValidateAttempt(ctx context.Context, key model.AttemptKey) error {
	ref := runModel.AttemptRef{RunId: key.RunId, Task: key.Task, Attempt: key.Attempt}
	if _, _, err := u.runSvc.GetAttempt(ctx, ref, true); err != nil {
		return fmt.Errorf("runSvc.GetAttempt: %w", err)
	}
	return nil
}

func (u *Usecase) Append(key model.AttemptKey, entries []model.Entry) error {
	if err := u.svc.Append(key, entries); err != nil {
		return fmt.Errorf("svc.Append: %w", err)
	}
	return nil
}

func (u *Usecase) Read(ctx context.Context, key model.AttemptKey, follow bool, fn func([]model.Entry) error) error {
	if err := u.ValidateAttempt(ctx, key); err != nil {
		return err
	}
	if err := u.svc.Read(ctx, key, follow, fn); err != nil {
		return fmt.Errorf("svc.Read: %w", err)
	}
	return nil
}
