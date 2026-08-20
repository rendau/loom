package loom

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendau/loom/sdk/streamstore"
)

func newTestFsStore(t *testing.T) *fsStore {
	t.Helper()

	s, err := newFsStore(t.TempDir())
	require.NoError(t, err)
	return s
}

func testArtifactRef() ArtifactRef {
	return ArtifactRef{RunID: "r1", Task: "t1", Attempt: 1, Name: "a1"}
}

func TestFsStoreFollowRead(t *testing.T) {
	store := newTestFsStore(t)
	ref := testArtifactRef()

	// читатель стартует раньше писателя — OpenRead ждёт появления артефакта
	got := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		r, err := store.OpenRead(context.Background(), ref)
		if err != nil {
			readErr <- err
			return
		}
		b, err := io.ReadAll(r)
		if err != nil {
			readErr <- err
			return
		}
		got <- b
	}()

	time.Sleep(50 * time.Millisecond)

	w, err := store.OpenWrite(context.Background(), ref)
	require.NoError(t, err)

	_, err = w.Write([]byte("hello "))
	require.NoError(t, err)
	_, err = w.Write([]byte("world"))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	select {
	case b := <-got:
		assert.Equal(t, "hello world", string(b))
	case err = <-readErr:
		t.Fatalf("read failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}

func TestFsStoreAbort(t *testing.T) {
	store := newTestFsStore(t)
	ref := testArtifactRef()

	w, err := store.OpenWrite(context.Background(), ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("partial"))
	require.NoError(t, err)

	readErr := make(chan error, 1)
	go func() {
		r, err := store.OpenRead(context.Background(), ref)
		if err != nil {
			readErr <- err
			return
		}
		_, err = io.ReadAll(r)
		readErr <- err
	}()

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, w.Abort())

	select {
	case err = <-readErr:
		assert.ErrorIs(t, err, streamstore.ErrAborted)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	// запись в abort'нутый поток невозможна
	_, err = w.Write([]byte("more"))
	assert.Error(t, err)
}

func TestFsStoreSecondWriter(t *testing.T) {
	store := newTestFsStore(t)
	ref := testArtifactRef()

	_, err := store.OpenWrite(context.Background(), ref)
	require.NoError(t, err)

	_, err = store.OpenWrite(context.Background(), ref)
	assert.ErrorIs(t, err, streamstore.ErrAlreadyExists)
}

func TestFsStoreReaderCtxCancel(t *testing.T) {
	store := newTestFsStore(t)
	ref := testArtifactRef()

	ctx, cancel := context.WithCancel(context.Background())

	readErr := make(chan error, 1)
	go func() {
		_, err := store.OpenRead(ctx, ref) // писателя нет — ждём появления
		readErr <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-readErr:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}
