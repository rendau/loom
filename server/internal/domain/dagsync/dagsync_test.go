package dagsync

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

type fakeResolver struct {
	digest string
	err    error
	calls  int
}

func (f *fakeResolver) ResolveDigest(context.Context, string) (string, error) {
	f.calls++
	return f.digest, f.err
}

type fakeEnqueuer struct {
	err   error
	calls int
}

func (f *fakeEnqueuer) EnqueueAuto(_ context.Context, _, dagName string) error {
	if dagName == "" {
		panic("dagsync must pass known dag name")
	}
	f.calls++
	return f.err
}

func dag(image, pinned string) *dagModel.Main {
	return &dagModel.Main{Name: "d1", Image: image, ImageDigest: pinned, AutoUpdate: true}
}

func TestSyncDag(t *testing.T) {
	ctx := context.Background()

	t.Run("digest unchanged — no re-register", func(t *testing.T) {
		resolver := &fakeResolver{digest: "sha256:aaa"}
		enqueuer := &fakeEnqueuer{}
		s := New(nil, resolver, enqueuer, 0)

		require.NoError(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d@sha256:aaa")))
		assert.Equal(t, 1, resolver.calls)
		assert.Equal(t, 0, enqueuer.calls)
	})

	t.Run("digest changed — re-register enqueued", func(t *testing.T) {
		resolver := &fakeResolver{digest: "sha256:bbb"}
		enqueuer := &fakeEnqueuer{}
		s := New(nil, resolver, enqueuer, 0)

		require.NoError(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d@sha256:aaa")))
		assert.Equal(t, 1, enqueuer.calls)
	})

	t.Run("pinned image ref — skipped", func(t *testing.T) {
		resolver := &fakeResolver{}
		s := New(nil, resolver, &fakeEnqueuer{}, 0)

		require.NoError(t, s.syncDag(ctx, dag("reg/d@sha256:aaa", "reg/d@sha256:aaa")))
		assert.Equal(t, 0, resolver.calls)
	})

	t.Run("unpinned registration — skipped", func(t *testing.T) {
		resolver := &fakeResolver{}
		s := New(nil, resolver, &fakeEnqueuer{}, 0)

		require.NoError(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d:latest")))
		assert.Equal(t, 0, resolver.calls)
	})

	t.Run("resolver error — propagated, no enqueue", func(t *testing.T) {
		resolver := &fakeResolver{err: fmt.Errorf("boom")}
		enqueuer := &fakeEnqueuer{}
		s := New(nil, resolver, enqueuer, 0)

		require.Error(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d@sha256:aaa")))
		assert.Equal(t, 0, enqueuer.calls)
	})

	t.Run("enqueue error — propagated", func(t *testing.T) {
		resolver := &fakeResolver{digest: "sha256:bbb"}
		enqueuer := &fakeEnqueuer{err: fmt.Errorf("db down")}
		s := New(nil, resolver, enqueuer, 0)

		require.Error(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d@sha256:aaa")))
	})
}
