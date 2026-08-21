package streamstore

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRef() Ref {
	return Ref{RunID: "run1", Task: "task1", Attempt: 1, Name: "out1"}
}

// readAll вычитывает артефакт целиком через Next.
func readAll(t *testing.T, r *Reader) ([]byte, error) {
	t.Helper()

	var res []byte
	buf := make([]byte, 8) // маленький буфер, чтобы прогнать цикл чтения
	for {
		n, err := r.Next(context.Background(), buf)
		res = append(res, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return res, nil
		}
		if err != nil {
			return res, err
		}
	}
}

func TestWriteCommitRead(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)

	_, err = w.Write([]byte("hello "))
	require.NoError(t, err)
	_, err = w.Write([]byte("world"))
	require.NoError(t, err)

	size, err := w.Commit()
	require.NoError(t, err)
	assert.Equal(t, int64(11), size)

	state, statSize, err := s.Stat(ref)
	require.NoError(t, err)
	assert.Equal(t, StateCommitted, state)
	assert.Equal(t, int64(11), statSize)

	r, err := s.OpenRead(context.Background(), ref, 0, false)
	require.NoError(t, err)
	defer r.Close()

	b, err := readAll(t, r)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(b))
}

func TestReadWithOffset(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("hello world"))
	require.NoError(t, err)
	_, err = w.Commit()
	require.NoError(t, err)

	r, err := s.OpenRead(context.Background(), ref, 6, false)
	require.NoError(t, err)
	defer r.Close()

	b, err := readAll(t, r)
	require.NoError(t, err)
	assert.Equal(t, "world", string(b))
}

func TestFollowReader(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("part1;"))
	require.NoError(t, err)

	// follow-читатель стартует до завершения записи
	got := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		r, err := s.OpenRead(context.Background(), ref, 0, true)
		if err != nil {
			readErr <- err
			return
		}
		defer r.Close()

		b, err := readAll(t, r)
		if err != nil {
			readErr <- err
			return
		}
		got <- b
	}()

	time.Sleep(50 * time.Millisecond) // дать читателю догнать хвост
	_, err = w.Write([]byte("part2"))
	require.NoError(t, err)
	_, err = w.Commit()
	require.NoError(t, err)

	select {
	case b := <-got:
		assert.Equal(t, "part1;part2", string(b))
	case err = <-readErr:
		t.Fatalf("read failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestFollowReaderWaitsForCreation(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	// читатель открывается до того, как писатель создал артефакт
	got := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		r, err := s.OpenRead(context.Background(), ref, 0, true)
		if err != nil {
			readErr <- err
			return
		}
		defer r.Close()

		b, err := readAll(t, r)
		if err != nil {
			readErr <- err
			return
		}
		got <- b
	}()

	time.Sleep(50 * time.Millisecond) // дать читателю встать в ожидание

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("late data"))
	require.NoError(t, err)
	_, err = w.Commit()
	require.NoError(t, err)

	select {
	case b := <-got:
		assert.Equal(t, "late data", string(b))
	case err = <-readErr:
		t.Fatalf("read failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestFinishAttemptResolvesMissingArtifact(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	// читатель ждёт артефакт, который никто не создаст
	readErr := make(chan error, 1)
	go func() {
		_, err := s.OpenRead(context.Background(), ref, 0, true)
		readErr <- err
	}()

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, s.FinishAttempt(ref.AttemptKey()))

	select {
	case err = <-readErr:
		assert.ErrorIs(t, err, ErrNotFound)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// после завершения попытки — ErrNotFound сразу, без ожидания
	_, err = s.OpenRead(context.Background(), ref, 0, true)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFinishAttemptAbortsLeftovers(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("doomed"))
	require.NoError(t, err)

	require.NoError(t, s.FinishAttempt(ref.AttemptKey()))

	_, err = w.Write([]byte("late"))
	assert.ErrorIs(t, err, ErrNotWriting)

	_, err = s.OpenRead(context.Background(), ref, 0, true)
	assert.ErrorIs(t, err, ErrAborted)

	// запись в завершённую попытку запрещена
	ref.Name = "out2"
	_, err = s.BeginWrite(ref)
	assert.ErrorIs(t, err, ErrAttemptFinished)
}

func TestDoneMarkerSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	require.NoError(t, err)
	ref := testRef()
	require.NoError(t, s.FinishAttempt(ref.AttemptKey()))

	// «рестарт»: новый Store на том же каталоге видит маркер
	s2, err := New(dir)
	require.NoError(t, err)

	_, err = s2.OpenRead(context.Background(), ref, 0, true)
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = s2.BeginWrite(ref)
	assert.ErrorIs(t, err, ErrAttemptFinished)
}

func TestAbortPropagatesToReader(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("partial"))
	require.NoError(t, err)

	readErr := make(chan error, 1)
	go func() {
		r, err := s.OpenRead(context.Background(), ref, 0, true)
		if err != nil {
			readErr <- err
			return
		}
		defer r.Close()

		_, err = readAll(t, r)
		readErr <- err
	}()

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, w.Abort())

	select {
	case err = <-readErr:
		assert.ErrorIs(t, err, ErrAborted)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// новое чтение abort'нутого артефакта — сразу ошибка
	_, err = s.OpenRead(context.Background(), ref, 0, true)
	assert.ErrorIs(t, err, ErrAborted)

	// повторный Abort идемпотентен
	require.NoError(t, w.Abort())
}

func TestNonFollowReadOfWritingStream(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("available"))
	require.NoError(t, err)

	// без follow читаем то, что доступно сейчас, и получаем EOF
	r, err := s.OpenRead(context.Background(), ref, 0, false)
	require.NoError(t, err)
	defer r.Close()

	b, err := readAll(t, r)
	require.NoError(t, err)
	assert.Equal(t, "available", string(b))
}

func TestAlreadyExists(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)

	// пока запись активна
	_, err = s.BeginWrite(ref)
	assert.ErrorIs(t, err, ErrAlreadyExists)

	_, err = w.Commit()
	require.NoError(t, err)

	// и после commit — той же попыткой писать нельзя
	_, err = s.BeginWrite(ref)
	assert.ErrorIs(t, err, ErrAlreadyExists)

	// новая попытка — можно
	ref.Attempt = 2
	_, err = s.BeginWrite(ref)
	require.NoError(t, err)
}

func TestStaleWritingAfterRestart(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("doomed"))
	require.NoError(t, err)

	// процесс «упал»: новый Store на том же каталоге, стрим без писателя
	s2, err := New(dir)
	require.NoError(t, err)

	_, err = s2.OpenRead(context.Background(), ref, 0, true)
	assert.ErrorIs(t, err, ErrAborted)

	state, _, err := s2.Stat(ref)
	require.NoError(t, err)
	assert.Equal(t, StateAborted, state)
}

func TestResumeWriteAfterRestart(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("first|"))
	require.NoError(t, err)

	// процесс «упал»: новый Store на том же каталоге; владелец стрима
	// возобновляет запись вместо ленивого лечения в aborted
	s2, err := New(dir)
	require.NoError(t, err)

	writing, err := s2.ListWriting()
	require.NoError(t, err)
	assert.Equal(t, []Ref{ref}, writing)

	w2, err := s2.ResumeWrite(ref)
	require.NoError(t, err)
	_, err = w2.Write([]byte("second"))
	require.NoError(t, err)
	_, err = w2.Commit()
	require.NoError(t, err)

	r, err := s2.OpenRead(context.Background(), ref, 0, false)
	require.NoError(t, err)
	defer r.Close()
	data, err := readAll(t, r)
	require.NoError(t, err)
	assert.Equal(t, "first|second", string(data))

	// возобновлённый стрим больше не числится оборванным
	writing, err = s2.ListWriting()
	require.NoError(t, err)
	assert.Empty(t, writing)
}

func TestResumeWriteRejectsFinished(t *testing.T) {
	dir := t.TempDir()

	s, err := New(dir)
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Commit()
	require.NoError(t, err)

	s2, err := New(dir)
	require.NoError(t, err)

	// закоммиченный стрим не резюмится
	_, err = s2.ResumeWrite(ref)
	assert.ErrorIs(t, err, ErrAlreadyExists)

	// активная запись (тот же процесс) — тоже
	ref2 := Ref{RunID: "run1", Task: "task1", Attempt: 2, Name: "out1"}
	_, err = s2.BeginWrite(ref2)
	require.NoError(t, err)
	_, err = s2.ResumeWrite(ref2)
	assert.ErrorIs(t, err, ErrAlreadyExists)
}

func TestAbortRef(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)
	ref := testRef()

	w, err := s.BeginWrite(ref)
	require.NoError(t, err)

	require.NoError(t, s.AbortRef(ref))

	// писатель обнаруживает abort на следующей записи
	_, err = w.Write([]byte("late"))
	assert.ErrorIs(t, err, ErrNotWriting)

	// идемпотентность
	require.NoError(t, s.AbortRef(ref))

	// на закоммиченный артефакт AbortRef не действует
	ref.Attempt = 2
	w, err = s.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Commit()
	require.NoError(t, err)
	assert.ErrorIs(t, s.AbortRef(ref), ErrNotWriting)
}

func TestDeleteRun(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)

	// закоммиченный артефакт
	committed := testRef()
	w, err := s.BeginWrite(committed)
	require.NoError(t, err)
	_, err = w.Commit()
	require.NoError(t, err)

	// активная запись в том же ране
	writing := Ref{RunID: "run1", Task: "task2", Attempt: 1, Name: "out1"}
	activeW, err := s.BeginWrite(writing)
	require.NoError(t, err)

	// завершённая попытка в том же ране
	require.NoError(t, s.FinishAttempt(AttemptKey{RunID: "run1", Task: "task3", Attempt: 1}))

	// артефакт другого рана не должен пострадать
	other := Ref{RunID: "run2", Task: "task1", Attempt: 1, Name: "out1"}
	w, err = s.BeginWrite(other)
	require.NoError(t, err)
	_, err = w.Commit()
	require.NoError(t, err)

	require.NoError(t, s.DeleteRun("run1"))

	_, _, err = s.Stat(committed)
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = activeW.Write([]byte("late"))
	assert.ErrorIs(t, err, ErrNotWriting)

	// маркер завершённости стёрт вместе с раном — попытку можно писать заново
	_, err = s.BeginWrite(Ref{RunID: "run1", Task: "task3", Attempt: 1, Name: "out1"})
	require.NoError(t, err)

	state, _, err := s.Stat(other)
	require.NoError(t, err)
	assert.Equal(t, StateCommitted, state)
}

func TestReaderCtxCancelWhileWaitingForCreation(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	readErr := make(chan error, 1)
	go func() {
		_, err := s.OpenRead(ctx, testRef(), 0, true) // писателя нет — ждём
		readErr <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err = <-readErr:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestInvalidRef(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)

	_, err = s.BeginWrite(Ref{RunID: "../escape", Task: "t", Attempt: 1, Name: "n"})
	assert.ErrorIs(t, err, ErrInvalidRef)

	_, err = s.OpenRead(context.Background(), Ref{RunID: "r", Task: "t", Attempt: 0, Name: "n"}, 0, false)
	assert.ErrorIs(t, err, ErrInvalidRef)

	_, err = s.OpenRead(context.Background(), testRef(), -1, false)
	assert.ErrorIs(t, err, ErrInvalidRef)

	_, _, err = s.Stat(Ref{RunID: "r", Task: "t", Attempt: 1, Name: "a/b"})
	assert.ErrorIs(t, err, ErrInvalidRef)

	err = s.FinishAttempt(AttemptKey{RunID: "r", Task: "t", Attempt: 0})
	assert.ErrorIs(t, err, ErrInvalidRef)
}

func TestNotFound(t *testing.T) {
	s, err := New(t.TempDir())
	require.NoError(t, err)

	_, err = s.OpenRead(context.Background(), testRef(), 0, false)
	assert.ErrorIs(t, err, ErrNotFound)

	_, _, err = s.Stat(testRef())
	assert.ErrorIs(t, err, ErrNotFound)
}
