// Package tasklog — приём, хранение и чтение логов тасков на artifact-сервере.
//
// Хранилище — та же файловая стейт-машина streamstore, что и у артефактов,
// но отдельным каталогом: у лог-стримов свой жизненный цикл (commit делает
// control plane при финализации попытки, а FinishAttempt артефактов,
// вызываемый SDK, не должен abort'ить ещё открытый лог). Лог попытки —
// стрим (run_id, task, attempt, "log") в формате JSONL.
//
// Доставка — без потерь: строки нумеруются клиентом сквозным seq (с нуля),
// сервис хранит число записанных строк каждого открытого стрима и молча
// пропускает строки батча с seq меньше него — повторная досылка после
// реконнекта не дублирует лог. Число уже записанных строк отдаётся клиенту
// на header (NextSeq), чтобы тот отбросил подтверждённый префикс буфера.
package tasklog

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	json "github.com/goccy/go-json"

	"github.com/rendau/loom/sdk/streamstore"

	"github.com/rendau/loom/artifact/internal/domain/tasklog/model"
)

const logStreamName = "log"

const readChunkSize = 64 * 1024

// ErrSeqGap — батч начинается дальше числа записанных строк: клиент
// отбросил неподтверждённые строки (нарушение протокола доставки).
var ErrSeqGap = errors.New("task log seq gap")

// entryDTO — формат хранения строки в JSONL-стриме.
type entryDTO struct {
	Ts     int64  `json:"ts"`
	Source string `json:"src"`
	Line   string `json:"line"`
}

// writerState — открытый лог-стрим и число записанных в него строк.
type writerState struct {
	w     *streamstore.Writer
	count int64
}

type Service struct {
	store *streamstore.Store

	mu      sync.Mutex
	writers map[model.AttemptKey]*writerState
}

func New(dir string) (*Service, error) {
	store, err := streamstore.New(dir)
	if err != nil {
		return nil, fmt.Errorf("streamstore.New: %w", err)
	}

	s := &Service{
		store:   store,
		writers: map[model.AttemptKey]*writerState{},
	}
	s.recoverWriters()

	return s, nil
}

// recoverWriters переоткрывает лог-стримы, оборванные рестартом сервера:
// иначе streamstore лениво пометил бы их aborted и лог живого attempt'а стал
// бы нечитаемым. Запись продолжается с места обрыва (число строк
// пересчитывается по файлу); commit сделает обычная финализация попытки.
func (s *Service) recoverWriters() {
	refs, err := s.store.ListWriting()
	if err != nil {
		slog.Warn("task log recovery: list writing streams", "error", err)
		return
	}

	resumed := 0
	for _, r := range refs {
		if r.Name != logStreamName {
			continue
		}
		st, err := s.resumeWriter(r)
		if err != nil {
			slog.Warn("task log recovery: resume stream",
				"run_id", r.RunID, "task", r.Task, "attempt", r.Attempt, "error", err)
			continue
		}
		s.writers[model.AttemptKey{RunId: r.RunID, Task: r.Task, Attempt: r.Attempt}] = st
		resumed++
	}

	if resumed > 0 {
		slog.Info("task log streams resumed after restart", "count", resumed)
	}
}

// resumeWriter возобновляет запись оборванного стрима, пересчитав число уже
// записанных строк по данным файла.
func (s *Service) resumeWriter(ref streamstore.Ref) (*writerState, error) {
	count, err := s.countEntries(ref)
	if err != nil {
		return nil, err
	}

	w, err := s.store.ResumeWrite(ref)
	if err != nil {
		return nil, err
	}

	return &writerState{w: w, count: count}, nil
}

// countEntries считает завершённые ('\n') строки лог-стрима — число
// записанных строк при возобновлении после рестарта. Незавершённый хвост
// без '\n' возможен только при падении машины посреди write() (рестарт
// процесса завершённые write() не рвёт): дозапись идёт в конец, хвост
// срастётся со следующей строкой в одну битую — читатель такие строки
// пропускает, теряется максимум одна строка на инцидент.
func (s *Service) countEntries(ref streamstore.Ref) (int64, error) {
	r, err := s.store.OpenRead(context.Background(), ref, 0, false)
	if err != nil {
		return 0, fmt.Errorf("open for count: %w", err)
	}
	defer func() { _ = r.Close() }()

	var count int64
	buf := make([]byte, readChunkSize)
	for {
		n, err := r.Next(context.Background(), buf)
		count += int64(bytes.Count(buf[:n], []byte("\n")))
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return 0, fmt.Errorf("count entries: %w", err)
		}
	}
}

func ref(key model.AttemptKey) streamstore.Ref {
	return streamstore.Ref{RunID: key.RunId, Task: key.Task, Attempt: key.Attempt, Name: logStreamName}
}

// NextSeq лениво открывает лог-стрим попытки и возвращает число уже
// записанных строк — ack на header push-стрима.
func (s *Service) NextSeq(key model.AttemptKey) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.writerLocked(key)
	if err != nil {
		return 0, err
	}
	return st.count, nil
}

// Append дописывает строки батча с дедупликацией по seq: строки с номером
// меньше числа записанных пропускаются (повторная досылка после реконнекта),
// батч с началом дальше записанного — ErrSeqGap. Возвращает новое число
// записанных строк (next_seq для ack).
func (s *Service) Append(key model.AttemptKey, firstSeq int64, entries []model.Entry) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, err := s.writerLocked(key)
	if err != nil {
		return 0, err
	}

	if firstSeq > st.count {
		return 0, fmt.Errorf("%w: batch starts at %d, stored %d", ErrSeqGap, firstSeq, st.count)
	}
	fresh := entries[min(st.count-firstSeq, int64(len(entries))):]
	if len(fresh) == 0 {
		return st.count, nil
	}

	if err = writeEntries(st.w, fresh); err != nil {
		return 0, err
	}
	st.count += int64(len(fresh))

	return st.count, nil
}

func (s *Service) writerLocked(key model.AttemptKey) (*writerState, error) {
	if st, ok := s.writers[key]; ok {
		return st, nil
	}

	st, err := s.openWriter(key)
	if err != nil {
		return nil, err
	}

	s.writers[key] = st
	return st, nil
}

// openWriter открывает новый лог-стрим попытки, а существующий незавершённый
// (например, лениво не восстановленный после рестарта) — возобновляет.
func (s *Service) openWriter(key model.AttemptKey) (*writerState, error) {
	w, err := s.store.BeginWrite(ref(key))
	switch {
	case err == nil:
		return &writerState{w: w}, nil
	case errors.Is(err, streamstore.ErrAlreadyExists):
		st, resumeErr := s.resumeWriter(ref(key))
		if resumeErr != nil {
			return nil, fmt.Errorf("resume log stream: %w", errors.Join(resumeErr, err))
		}
		return st, nil
	default:
		return nil, fmt.Errorf("store.BeginWrite: %w", err)
	}
}

// Finish закрывает лог попытки: дописывает финальные строки (исход попытки)
// и коммитит стрим — follow-читатели получают EOF. Идемпотентен: у
// завершённого лога — no-op.
func (s *Service) Finish(key model.AttemptKey, final []model.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.writers[key]
	if !ok {
		var err error
		st, err = s.openWriter(key)
		switch {
		case errors.Is(err, streamstore.ErrAlreadyExists), errors.Is(err, streamstore.ErrAttemptFinished):
			return nil // лог уже финализирован
		case err != nil:
			return err
		}
	}
	delete(s.writers, key)

	writeErr := writeEntries(st.w, final)

	if _, err := st.w.Commit(); err != nil {
		return errors.Join(writeErr, fmt.Errorf("commit log stream: %w", err))
	}

	// маркер завершённой попытки: follow-читатели несозданного лог-стрима
	// получат NOT_FOUND вместо вечного ожидания, поздний push — отказ
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

// DeleteRun удаляет все логи рана (retention). Активные писатели рана
// закрываются: их стримы abort'ит streamstore.DeleteRun.
func (s *Service) DeleteRun(runId string) error {
	s.mu.Lock()
	for key := range s.writers {
		if key.RunId == runId {
			delete(s.writers, key)
		}
	}
	s.mu.Unlock()

	if err := s.store.DeleteRun(runId); err != nil {
		return fmt.Errorf("store.DeleteRun: %w", err)
	}
	return nil
}

// Read читает лог попытки, пропустив первые afterSeq строк, отдавая строки
// батчами в fn; при follow=true блокируется до финализации лога, отдавая
// новые строки по мере поступления.
func (s *Service) Read(ctx context.Context, key model.AttemptKey, afterSeq int64, follow bool, fn func([]model.Entry) error) error {
	r, err := s.store.OpenRead(ctx, ref(key), 0, follow)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	skip := afterSeq
	buf := make([]byte, readChunkSize)
	var pending []byte

	for {
		n, err := r.Next(ctx, buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)

			entries, rest := decodeLines(pending, &skip)
			pending = rest

			if len(entries) > 0 {
				if fnErr := fn(entries); fnErr != nil {
					return fnErr
				}
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				// незавершённая последняя строка возможна только при падении
				// сервера посреди записи — молча отбрасываем хвост
				return nil
			}
			return err
		}
	}
}

// decodeLines разбирает завершённые JSONL-строки, пропуская первые *skip,
// и возвращает остаток буфера. Битая строка (склейка после падения машины
// посреди записи) пропускается — одна потерянная строка не должна делать
// нечитаемым весь лог.
func decodeLines(data []byte, skip *int64) ([]model.Entry, []byte) {
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
		if *skip > 0 {
			*skip--
			continue
		}

		var dto entryDTO
		if err := json.Unmarshal(line, &dto); err != nil {
			continue
		}
		entries = append(entries, model.Entry{TsUnixMs: dto.Ts, Source: dto.Source, Line: dto.Line})
	}

	return entries, data
}
