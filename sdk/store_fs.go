package loom

import (
	"context"
	"io"

	"github.com/rendau/loom/sdk/streamstore"
)

// fsStore адаптирует streamstore.Store к порту artifactStore. Используется в
// локальном режиме: артефакты лежат файлами и остаются после рана — их можно
// изучать. Семантика та же, что у artifact-сервера, вплоть до общего кода.
type fsStore struct {
	store *streamstore.Store
}

func newFsStore(dir string) (*fsStore, error) {
	store, err := streamstore.New(dir)
	if err != nil {
		return nil, err
	}

	return &fsStore{store: store}, nil
}

func (s *fsStore) OpenWrite(_ context.Context, ref ArtifactRef) (ArtifactWriter, error) {
	w, err := s.store.BeginWrite(encodeStoreRef(ref))
	if err != nil {
		return nil, err
	}

	return &fsWriter{w: w}, nil
}

func (s *fsStore) OpenRead(ctx context.Context, ref ArtifactRef) (io.ReadCloser, error) {
	r, err := s.store.OpenRead(ctx, encodeStoreRef(ref), 0, true)
	if err != nil {
		return nil, err
	}

	return &fsReader{ctx: ctx, r: r}, nil
}

// finishAttempt помечает попытку таска завершённой: abort остаткам записей,
// маркер на диске, разблокировка читателей несозданных артефактов.
func (s *fsStore) finishAttempt(runID, task string, attempt int) error {
	return s.store.FinishAttempt(streamstore.AttemptKey{RunID: runID, Task: task, Attempt: int32(attempt)})
}

func encodeStoreRef(v ArtifactRef) streamstore.Ref {
	return streamstore.Ref{RunID: v.RunID, Task: v.Task, Attempt: int32(v.Attempt), Name: v.Name}
}

type fsWriter struct {
	w *streamstore.Writer
}

func (w *fsWriter) Write(p []byte) (int, error) {
	return w.w.Write(p)
}

func (w *fsWriter) Commit() error {
	_, err := w.w.Commit()
	return err
}

func (w *fsWriter) Abort() error {
	return w.w.Abort()
}

type fsReader struct {
	ctx context.Context
	r   *streamstore.Reader
}

func (r *fsReader) Read(p []byte) (int, error) {
	return r.r.Next(r.ctx, p)
}

func (r *fsReader) Close() error {
	return r.r.Close()
}
