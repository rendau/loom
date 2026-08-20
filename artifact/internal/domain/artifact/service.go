package artifact

import (
	"context"

	"github.com/rendau/loom/sdk/streamstore"
)

// Стейт-машина стримов (writing → committed | aborted, follow-читатели,
// stale-writing → aborted) живёт в sdk/streamstore — общая с локальным
// режимом SDK, чтобы семантика обмена данными совпадала вплоть до кода.
// Здесь — доменный фасад artifact-сервера: сюда лягут retention-политики,
// скоупинг токенов attempt'ов и метрики.

var (
	ErrInvalidRef      = streamstore.ErrInvalidRef
	ErrNotFound        = streamstore.ErrNotFound
	ErrAlreadyExists   = streamstore.ErrAlreadyExists
	ErrAborted         = streamstore.ErrAborted
	ErrNotWriting      = streamstore.ErrNotWriting
	ErrAttemptFinished = streamstore.ErrAttemptFinished
)

type (
	State      = streamstore.State
	Ref        = streamstore.Ref
	AttemptKey = streamstore.AttemptKey
	Writer     = streamstore.Writer
	Reader     = streamstore.Reader
)

const (
	StateWriting   = streamstore.StateWriting
	StateCommitted = streamstore.StateCommitted
	StateAborted   = streamstore.StateAborted
)

type Service struct {
	store *streamstore.Store
}

func New(dir string) (*Service, error) {
	store, err := streamstore.New(dir)
	if err != nil {
		return nil, err
	}

	return &Service{store: store}, nil
}

func (s *Service) BeginWrite(ref Ref) (*Writer, error) {
	return s.store.BeginWrite(ref)
}

func (s *Service) OpenRead(ctx context.Context, ref Ref, offset int64, follow bool) (*Reader, error) {
	return s.store.OpenRead(ctx, ref, offset, follow)
}

func (s *Service) Stat(ref Ref) (State, int64, error) {
	return s.store.Stat(ref)
}

func (s *Service) AbortRef(ref Ref) error {
	return s.store.AbortRef(ref)
}

// FinishAttempt будет вызывать control plane по завершении attempt'а.
func (s *Service) FinishAttempt(key AttemptKey) error {
	return s.store.FinishAttempt(key)
}

func (s *Service) DeleteRun(runID string) error {
	return s.store.DeleteRun(runID)
}
