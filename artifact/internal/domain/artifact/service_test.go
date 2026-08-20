package artifact

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Полная стейт-машина покрыта тестами sdk/streamstore; здесь — smoke-тест
// проводки фасада.
func TestServiceSmoke(t *testing.T) {
	svc, err := New(t.TempDir())
	require.NoError(t, err)
	ref := Ref{RunID: "run1", Task: "task1", Attempt: 1, Name: "out1"}

	w, err := svc.BeginWrite(ref)
	require.NoError(t, err)
	_, err = w.Write([]byte("payload"))
	require.NoError(t, err)
	size, err := w.Commit()
	require.NoError(t, err)
	assert.Equal(t, int64(7), size)

	state, statSize, err := svc.Stat(ref)
	require.NoError(t, err)
	assert.Equal(t, StateCommitted, state)
	assert.Equal(t, int64(7), statSize)

	r, err := svc.OpenRead(context.Background(), ref, 0, false)
	require.NoError(t, err)
	defer r.Close()

	var got []byte
	buf := make([]byte, 32)
	for {
		n, err := r.Next(context.Background(), buf)
		got = append(got, buf[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	assert.Equal(t, "payload", string(got))

	require.NoError(t, svc.DeleteRun("run1"))
	_, _, err = svc.Stat(ref)
	assert.ErrorIs(t, err, ErrNotFound)
}
