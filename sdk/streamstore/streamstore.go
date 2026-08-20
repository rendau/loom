// Package streamstore — файловое хранилище стримов артефактов с
// follow-семантикой.
//
// Это единственная реализация стейт-машины артефакта (writing → committed |
// aborted): её используют и artifact-сервер, и локальный режим SDK, поэтому
// семантика обмена данными локально и в проде совпадает вплоть до кода.
//
// Артефакт скоупится на попытку таска (run_id, task, attempt, name).
// Follow-читатели догоняют хвост пишущегося файла и ждут новых данных;
// io.EOF — только после commit, abort возвращает читателю ошибку. Читатель
// может открыться раньше писателя — OpenRead с follow ждёт появления
// артефакта, пока попытка-источник не завершена (FinishAttempt).
package streamstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"

	json "github.com/goccy/go-json"
	"github.com/samber/lo"
)

var (
	ErrInvalidRef      = errors.New("invalid_ref")
	ErrNotFound        = errors.New("artifact_not_found")
	ErrAlreadyExists   = errors.New("artifact_already_exists")
	ErrAborted         = errors.New("artifact_aborted")
	ErrNotWriting      = errors.New("artifact_not_writing")
	ErrAttemptFinished = errors.New("attempt_finished")
)

type State string

const (
	StateWriting   State = "writing"
	StateCommitted State = "committed"
	StateAborted   State = "aborted"
)

// doneMarker — файл-маркер завершённой попытки в каталоге попытки. Ведущая
// точка исключает коллизию с файлами артефактов: имя артефакта с неё
// начинаться не может.
const doneMarker = ".done"

// refPartRe защищает от path traversal: части ref становятся сегментами пути.
var refPartRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// Ref адресует артефакт; скоуп — попытка таска: ретрай пишет свои артефакты
// новой попыткой, не трогая данные прошлых.
type Ref struct {
	RunID   string
	Task    string
	Attempt int32
	Name    string
}

func (r Ref) AttemptKey() AttemptKey {
	return AttemptKey{RunID: r.RunID, Task: r.Task, Attempt: r.Attempt}
}

func (r Ref) validate() error {
	if err := r.AttemptKey().validate(); err != nil {
		return err
	}
	if !refPartRe.MatchString(r.Name) {
		return fmt.Errorf("%w: %+v", ErrInvalidRef, r)
	}
	return nil
}

// AttemptKey адресует попытку таска — скоуп всех её артефактов.
type AttemptKey struct {
	RunID   string
	Task    string
	Attempt int32
}

func (k AttemptKey) validate() error {
	if !refPartRe.MatchString(k.RunID) || !refPartRe.MatchString(k.Task) || k.Attempt < 1 {
		return fmt.Errorf("%w: %+v", ErrInvalidRef, k)
	}
	return nil
}

// meta — sidecar-файл состояния артефакта рядом с данными.
type meta struct {
	State State `json:"state"`
	Size  int64 `json:"size"`
}

// Store — файловое хранилище стримов артефактов.
//
// Стримы, оставшиеся writing без активного писателя (падение процесса),
// лениво считаются aborted.
type Store struct {
	dir string

	mu         sync.Mutex
	createCond *sync.Cond // будит follow-читателей, ждущих появления артефакта
	active     map[Ref]*stream
	finished   map[AttemptKey]bool // кэш маркеров завершённых попыток
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir data dir: %w", err)
	}

	s := &Store{
		dir:      dir,
		active:   map[Ref]*stream{},
		finished: map[AttemptKey]bool{},
	}
	s.createCond = sync.NewCond(&s.mu)

	return s, nil
}

// stream — активная запись; синхронизирует писателя и follow-читателей.
type stream struct {
	mu    sync.Mutex
	cond  *sync.Cond
	file  *os.File
	size  int64
	state State
}

func (s *Store) attemptDir(key AttemptKey) string {
	return filepath.Join(s.dir, key.RunID, key.Task, strconv.Itoa(int(key.Attempt)))
}

func (s *Store) dataPath(ref Ref) string {
	return filepath.Join(s.attemptDir(ref.AttemptKey()), ref.Name+".data")
}

func (s *Store) metaPath(ref Ref) string {
	return filepath.Join(s.attemptDir(ref.AttemptKey()), ref.Name+".meta.json")
}

func (s *Store) readMeta(ref Ref) (meta, error) {
	raw, err := os.ReadFile(s.metaPath(ref))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return meta{}, ErrNotFound
		}
		return meta{}, fmt.Errorf("read meta: %w", err)
	}

	var m meta
	if err = json.Unmarshal(raw, &m); err != nil {
		return meta{}, fmt.Errorf("decode meta: %w", err)
	}

	return m, nil
}

// writeMeta пишет мету атомарно (temp + rename).
func (s *Store) writeMeta(ref Ref, m meta) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode meta: %w", err)
	}

	path := s.metaPath(ref)
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("write meta: %w", err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename meta: %w", err)
	}

	return nil
}

// metaLocked читает мету под s.mu и лечит stale-writing (записи, оборванные
// падением процесса) в aborted.
func (s *Store) metaLocked(ref Ref) (meta, error) {
	m, err := s.readMeta(ref)
	if err != nil {
		return meta{}, err
	}

	if m.State == StateWriting && s.active[ref] == nil {
		m.State = StateAborted
		if err = s.writeMeta(ref, m); err != nil {
			return meta{}, err
		}
	}

	return m, nil
}

// attemptFinishedLocked проверяет завершённость попытки: кэш в памяти или
// маркер на диске (переживает рестарт процесса).
func (s *Store) attemptFinishedLocked(key AttemptKey) bool {
	if s.finished[key] {
		return true
	}

	if _, err := os.Stat(filepath.Join(s.attemptDir(key), doneMarker)); err == nil {
		s.finished[key] = true
		return true
	}

	return false
}

func (s *Store) removeActive(ref Ref) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.active, ref)
}

// finishStream завершает активную запись (commit или abort) и публикует
// финальное состояние читателям.
func (s *Store) finishStream(ref Ref, st *stream, state State) (int64, error) {
	st.mu.Lock()

	if st.state != StateWriting {
		st.mu.Unlock()
		return 0, ErrNotWriting
	}

	// сначала данные и мета на диск, затем финальное состояние читателям
	var errs []error
	if state == StateCommitted {
		if err := st.file.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("sync data file: %w", err))
		}
	}
	if err := st.file.Close(); err != nil {
		errs = append(errs, fmt.Errorf("close data file: %w", err))
	}
	if len(errs) > 0 {
		state = StateAborted // не смогли зафиксировать данные — поток невалиден
	}
	if err := s.writeMeta(ref, meta{State: state, Size: st.size}); err != nil {
		errs = append(errs, err)
		state = StateAborted
	}

	st.state = state
	size := st.size
	st.cond.Broadcast()
	st.mu.Unlock()

	s.removeActive(ref)

	if err := errors.Join(errs...); err != nil {
		return 0, err
	}
	return size, nil
}

// ── Запись ──────────────────────────────────────────────

// Writer — активная запись артефакта.
type Writer struct {
	store *Store
	ref   Ref
	st    *stream
}

// BeginWrite начинает запись артефакта. Повторная запись той же попытки
// запрещена — ретрай таска обязан идти новой попыткой; запись в завершённую
// попытку — тоже (ErrAttemptFinished).
func (s *Store) BeginWrite(ref Ref) (*Writer, error) {
	if err := ref.validate(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.attemptFinishedLocked(ref.AttemptKey()) {
		return nil, fmt.Errorf("%w: %+v", ErrAttemptFinished, ref)
	}
	if _, ok := s.active[ref]; ok {
		return nil, fmt.Errorf("%w: write is in progress", ErrAlreadyExists)
	}
	if _, err := s.readMeta(ref); err == nil {
		return nil, ErrAlreadyExists
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(s.dataPath(ref)), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir artifact dir: %w", err)
	}

	file, err := os.OpenFile(s.dataPath(ref), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create data file: %w", err)
	}

	if err = s.writeMeta(ref, meta{State: StateWriting}); err != nil {
		_ = file.Close()
		return nil, err
	}

	st := &stream{file: file, state: StateWriting}
	st.cond = sync.NewCond(&st.mu)
	s.active[ref] = st

	// будим follow-читателей, ждущих появления этого артефакта
	s.createCond.Broadcast()

	return &Writer{store: s, ref: ref, st: st}, nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.st.mu.Lock()
	defer w.st.mu.Unlock()

	if w.st.state != StateWriting {
		return 0, ErrNotWriting
	}

	n, err := w.st.file.Write(p)
	w.st.size += int64(n)
	if n > 0 {
		w.st.cond.Broadcast()
	}
	if err != nil {
		return n, fmt.Errorf("write data file: %w", err)
	}

	return n, nil
}

// Commit фиксирует артефакт: читатели дочитают его до конца и получат EOF.
func (w *Writer) Commit() (int64, error) {
	return w.store.finishStream(w.ref, w.st, StateCommitted)
}

// Abort инвалидирует артефакт: follow-читатели получат ошибку.
// Идемпотентен: повторный вызов после Commit/Abort — no-op.
func (w *Writer) Abort() error {
	_, err := w.store.finishStream(w.ref, w.st, StateAborted)
	if errors.Is(err, ErrNotWriting) {
		return nil
	}
	return err
}

// ── Чтение ──────────────────────────────────────────────

// Reader читает артефакт; Next в follow-режиме блокируется, пока писатель
// не допишет новые данные, не сделает commit или abort.
type Reader struct {
	file   *os.File
	off    int64
	size   int64   // размер данных (для неактивных стримов)
	st     *stream // nil, если запись уже завершена
	follow bool
}

// OpenRead открывает чтение с offset. При follow=true ещё не созданный
// артефакт ожидается, пока его попытка-источник не завершится (тогда
// ErrNotFound); без follow отсутствие артефакта — сразу ErrNotFound.
func (s *Store) OpenRead(ctx context.Context, ref Ref, offset int64, follow bool) (*Reader, error) {
	if err := ref.validate(); err != nil {
		return nil, err
	}
	if offset < 0 {
		return nil, fmt.Errorf("%w: negative offset", ErrInvalidRef)
	}

	s.mu.Lock()

	var m meta
	for {
		var err error
		m, err = s.metaLocked(ref)
		if err == nil {
			break
		}
		if !errors.Is(err, ErrNotFound) || !follow || s.attemptFinishedLocked(ref.AttemptKey()) {
			s.mu.Unlock()
			return nil, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			s.mu.Unlock()
			return nil, ctxErr
		}

		// ждём появления артефакта; разбудит BeginWrite, FinishAttempt
		// или отмена контекста
		stop := context.AfterFunc(ctx, s.createCond.Broadcast)
		s.createCond.Wait()
		stop()
	}

	st := s.active[ref]
	s.mu.Unlock()

	if st == nil && m.State == StateAborted {
		return nil, ErrAborted
	}

	file, err := os.Open(s.dataPath(ref))
	if err != nil {
		return nil, fmt.Errorf("open data file: %w", err)
	}

	return &Reader{file: file, off: offset, size: m.Size, st: st, follow: follow}, nil
}

// Next читает следующую порцию в p. io.EOF — артефакт дочитан до конца:
// для закоммиченного это конец данных, для пишущегося без follow — конец
// доступных на данный момент данных.
func (r *Reader) Next(ctx context.Context, p []byte) (int, error) {
	avail, err := r.wait(ctx)
	if err != nil {
		return 0, err
	}
	if avail == 0 {
		return 0, io.EOF
	}

	n := int64(len(p))
	if n > avail {
		n = avail
	}

	read, err := r.file.ReadAt(p[:n], r.off)
	r.off += int64(read)
	if err != nil && !errors.Is(err, io.EOF) {
		return read, fmt.Errorf("read data file: %w", err)
	}

	return read, nil
}

// wait возвращает число байт, доступных с текущего offset; 0 — конец.
// В follow-режиме блокируется до новых данных, commit или abort.
func (r *Reader) wait(ctx context.Context) (int64, error) {
	if r.st == nil {
		avail := r.size - r.off
		if avail < 0 {
			avail = 0
		}
		return avail, nil
	}

	st := r.st

	st.mu.Lock()
	defer st.mu.Unlock()

	for {
		if r.off < st.size {
			return st.size - r.off, nil
		}

		switch st.state {
		case StateCommitted:
			return 0, nil
		case StateAborted:
			return 0, ErrAborted
		}

		if !r.follow {
			return 0, nil
		}

		if err := ctx.Err(); err != nil {
			return 0, err
		}

		// ждём новых данных; разбудит запись, commit/abort или отмена контекста
		stop := context.AfterFunc(ctx, st.cond.Broadcast)
		st.cond.Wait()
		stop()
	}
}

func (r *Reader) Close() error {
	return r.file.Close()
}

// ── Управление ──────────────────────────────────────────

// Stat возвращает состояние и текущий размер артефакта.
func (s *Store) Stat(ref Ref) (State, int64, error) {
	if err := ref.validate(); err != nil {
		return "", 0, err
	}

	s.mu.Lock()
	m, err := s.metaLocked(ref)
	st := s.active[ref]
	s.mu.Unlock()
	if err != nil {
		return "", 0, err
	}

	if st != nil {
		st.mu.Lock()
		m.State, m.Size = st.state, st.size
		st.mu.Unlock()
	}

	return m.State, m.Size, nil
}

// AbortRef abort'ит запись извне — control plane вызывает его для стримов
// умерших attempt'ов. Идемпотентен для уже abort'нутых; для закоммиченных
// возвращает ErrNotWriting.
func (s *Store) AbortRef(ref Ref) error {
	if err := ref.validate(); err != nil {
		return err
	}

	s.mu.Lock()
	st := s.active[ref]
	s.mu.Unlock()

	if st != nil {
		_, err := s.finishStream(ref, st, StateAborted)
		if err == nil || !errors.Is(err, ErrNotWriting) {
			return err
		}
		// запись завершилась параллельно — решаем по мете ниже
	}

	s.mu.Lock()
	m, err := s.metaLocked(ref)
	s.mu.Unlock()
	if err != nil {
		return err
	}

	if m.State == StateCommitted {
		return ErrNotWriting
	}

	return nil
}

// FinishAttempt помечает попытку завершённой: abort'ит её оставшиеся
// активные записи, ставит маркер на диске и будит читателей, ждущих так и
// не созданные артефакты (они получат ErrNotFound). Вызывается после
// завершения таска — локальным раннером или control plane'ом.
func (s *Store) FinishAttempt(key AttemptKey) error {
	if err := key.validate(); err != nil {
		return err
	}

	s.mu.Lock()
	leftovers := lo.PickBy(s.active, func(ref Ref, _ *stream) bool { return ref.AttemptKey() == key })
	s.mu.Unlock()

	for ref, st := range leftovers {
		_, err := s.finishStream(ref, st, StateAborted)
		if err != nil && !errors.Is(err, ErrNotWriting) {
			return err
		}
	}

	dir := s.attemptDir(key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir attempt dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, doneMarker), nil, 0o644); err != nil {
		return fmt.Errorf("write done marker: %w", err)
	}

	s.mu.Lock()
	s.finished[key] = true
	s.createCond.Broadcast()
	s.mu.Unlock()

	return nil
}

// DeleteRun удаляет все артефакты рана (retention). Активные записи рана
// предварительно abort'ятся.
func (s *Store) DeleteRun(runID string) error {
	if !refPartRe.MatchString(runID) {
		return fmt.Errorf("%w: run_id %q", ErrInvalidRef, runID)
	}

	s.mu.Lock()
	active := lo.PickBy(s.active, func(ref Ref, _ *stream) bool { return ref.RunID == runID })
	s.mu.Unlock()

	for ref, st := range active {
		_, err := s.finishStream(ref, st, StateAborted)
		if err != nil && !errors.Is(err, ErrNotWriting) {
			return err
		}
	}

	if err := os.RemoveAll(filepath.Join(s.dir, runID)); err != nil {
		return fmt.Errorf("remove run dir: %w", err)
	}

	s.mu.Lock()
	s.finished = lo.OmitBy(s.finished, func(k AttemptKey, _ bool) bool { return k.RunID == runID })
	s.mu.Unlock()

	return nil
}
