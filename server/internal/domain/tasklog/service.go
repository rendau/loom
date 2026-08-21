// Package tasklog — приём, хранение и чтение логов тасков.
//
// Хранилище — та же файловая стейт-машина streamstore, что и у артефактов
// (решение №5): лог попытки — стрим (run_id, task, attempt, "log") в
// формате JSONL. Follow-чтение для live-логов достаётся от streamstore
// бесплатно; commit стрима делает планировщик при финализации попытки —
// до этого он успевает дописать причину смерти пода (решение №7).
package tasklog

import (
	"context"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	json "github.com/goccy/go-json"

	"github.com/rendau/loom/sdk/streamstore"
	"github.com/rendau/loom/server/internal/domain/tasklog/model"
	"github.com/rendau/loom/server/internal/errs"
)

const logArtifactName = "log"

const readChunkSize = 64 * 1024

// entryDTO — формат хранения строки в JSONL-стриме.
type entryDTO struct {
	Ts     int64  `json:"ts"`
	Source string `json:"src"`
	Line   string `json:"line"`
}

type Service struct {
	store *streamstore.Store

	mu      sync.Mutex
	writers map[model.AttemptKey]*streamstore.Writer
}

func New(dir string) (*Service, error) {
	store, err := streamstore.New(dir)
	if err != nil {
		return nil, fmt.Errorf("streamstore.New: %w", err)
	}

	s := &Service{
		store:   store,
		writers: map[model.AttemptKey]*streamstore.Writer{},
	}
	s.recoverWriters()

	return s, nil
}

// recoverWriters переоткрывает лог-стримы, оборванные рестартом server:
// иначе streamstore лениво пометил бы их aborted и лог живого attempt'а стал
// бы нечитаемым. Запись продолжается с места обрыва; commit сделает обычная
// финализация попытки (умершую вместе с сервером попытку дофинализирует
// зомби-детект планировщика).
func (s *Service) recoverWriters() {
	refs, err := s.store.ListWriting()
	if err != nil {
		slog.Warn("task log recovery: list writing streams", "error", err)
		return
	}

	resumed := 0
	for _, r := range refs {
		if r.Name != logArtifactName {
			continue
		}
		w, err := s.store.ResumeWrite(r)
		if err != nil {
			slog.Warn("task log recovery: resume stream",
				"run_id", r.RunID, "task", r.Task, "attempt", r.Attempt, "error", err)
			continue
		}
		s.writers[model.AttemptKey{RunId: r.RunID, Task: r.Task, Attempt: r.Attempt}] = w
		resumed++
	}

	if resumed > 0 {
		slog.Info("task log streams resumed after restart", "count", resumed)
	}
}

func ref(key model.AttemptKey) streamstore.Ref {
	return streamstore.Ref{RunID: key.RunId, Task: key.Task, Attempt: key.Attempt, Name: logArtifactName}
}

// Append дописывает строки в лог-стрим попытки, лениво открывая запись.
func (s *Service) Append(key model.AttemptKey, entries []model.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	w, err := s.writerLocked(key)
	if err != nil {
		return err
	}
	return writeEntries(w, entries)
}

func (s *Service) writerLocked(key model.AttemptKey) (*streamstore.Writer, error) {
	if w, ok := s.writers[key]; ok {
		return w, nil
	}

	w, err := s.store.BeginWrite(ref(key))
	switch {
	case errors.Is(err, streamstore.ErrAlreadyExists), errors.Is(err, streamstore.ErrAttemptFinished):
		// лог этой попытки уже завершён (или пишется другим стримом) —
		// повторный push не принимаем
		return nil, fmt.Errorf("%w: %v", errs.LogAlreadyPushed, err)
	case err != nil:
		return nil, fmt.Errorf("store.BeginWrite: %w", err)
	}

	s.writers[key] = w
	return w, nil
}

// Finish закрывает лог попытки: дописывает финальные строки (например
// причину смерти пода) и коммитит стрим — follow-читатели получают EOF.
// Идемпотентен: у завершённого лога — no-op.
func (s *Service) Finish(key model.AttemptKey, final []model.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	w, ok := s.writers[key]
	if !ok {
		var err error
		w, err = s.store.BeginWrite(ref(key))
		switch {
		case errors.Is(err, streamstore.ErrAlreadyExists), errors.Is(err, streamstore.ErrAttemptFinished):
			return nil // лог уже финализирован
		case err != nil:
			return fmt.Errorf("store.BeginWrite: %w", err)
		}
	}
	delete(s.writers, key)

	writeErr := writeEntries(w, final)

	if _, err := w.Commit(); err != nil {
		return errors.Join(writeErr, fmt.Errorf("commit log stream: %w", err))
	}

	// маркер завершённой попытки: follow-читатели других (несозданных)
	// стримов попытки получат NOT_FOUND вместо вечного ожидания
	if err := s.store.FinishAttempt(streamstore.AttemptKey{RunID: key.RunId, Task: key.Task, Attempt: key.Attempt}); err != nil {
		return errors.Join(writeErr, fmt.Errorf("store.FinishAttempt: %w", err))
	}

	return writeErr
}

func writeEntries(w *streamstore.Writer, entries []model.Entry) error {
	if len(entries) == 0 {
		return nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range entries {
		if err := enc.Encode(entryDTO{Ts: e.TsUnixMs, Source: e.Source, Line: e.Line}); err != nil {
			return fmt.Errorf("encode log entry: %w", err)
		}
	}

	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write log stream: %w", err)
	}
	return nil
}

// Read читает лог попытки с начала, отдавая строки батчами в fn; при
// follow=true блокируется до завершения попытки, отдавая новые строки по
// мере поступления.
func (s *Service) Read(ctx context.Context, key model.AttemptKey, follow bool, fn func([]model.Entry) error) error {
	r, err := s.store.OpenRead(ctx, ref(key), 0, follow)
	switch {
	case errors.Is(err, streamstore.ErrNotFound):
		return fmt.Errorf("%w: %v", errs.ObjectNotFound, err)
	case errors.Is(err, streamstore.ErrAborted):
		return fmt.Errorf("%w: %v", errs.AttemptLogAborted, err)
	case err != nil:
		return fmt.Errorf("store.OpenRead: %w", err)
	}
	defer func() { _ = r.Close() }()

	buf := make([]byte, readChunkSize)
	var pending []byte

	for {
		n, err := r.Next(ctx, buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)

			entries, rest, decodeErr := decodeLines(pending)
			if decodeErr != nil {
				return decodeErr
			}
			pending = rest

			if len(entries) > 0 {
				if fnErr := fn(entries); fnErr != nil {
					return fnErr
				}
			}
		}

		switch {
		case errors.Is(err, io.EOF):
			// незавершённая последняя строка возможна только при падении
			// сервера посреди записи — молча отбрасываем хвост
			return nil
		case errors.Is(err, streamstore.ErrAborted):
			return fmt.Errorf("%w: %v", errs.AttemptLogAborted, err)
		case err != nil:
			return fmt.Errorf("read log stream: %w", err)
		}
	}
}

// decodeLines разбирает завершённые JSONL-строки, возвращая остаток буфера.
func decodeLines(data []byte) ([]model.Entry, []byte, error) {
	var entries []model.Entry

	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := data[:idx]
		data = data[idx+1:]

		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var dto entryDTO
		if err := json.Unmarshal(line, &dto); err != nil {
			return nil, nil, fmt.Errorf("decode log entry: %w", err)
		}
		entries = append(entries, model.Entry{TsUnixMs: dto.Ts, Source: dto.Source, Line: dto.Line})
	}

	return entries, data, nil
}
