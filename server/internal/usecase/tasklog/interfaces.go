package tasklog

import (
	"context"

	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	"github.com/rendau/loom/server/internal/domain/tasklog/model"
)

// ServiceI — чтение логов с artifact-сервера (artifactcli).
type ServiceI interface {
	ReadTaskLog(ctx context.Context, key model.AttemptKey, follow bool, fn func([]model.Entry) error) error
}

type RunServiceI interface {
	GetAttempt(ctx context.Context, ref runModel.AttemptRef, errNE bool) (*runModel.Attempt, bool, error)
}
