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

type fakeRegistrar struct {
	err   error
	calls int
}

func (f *fakeRegistrar) Register(_ context.Context, _ string, autoUpdate *bool) (*dagModel.Main, error) {
	if autoUpdate != nil {
		panic("dagsync must not touch auto_update flag")
	}
	f.calls++
	return nil, f.err
}

func dag(image, pinned string) *dagModel.Main {
	return &dagModel.Main{Name: "d1", Image: image, ImageDigest: pinned, AutoUpdate: true}
}

func TestSyncDag(t *testing.T) {
	ctx := context.Background()

	t.Run("digest unchanged — no re-register", func(t *testing.T) {
		resolver := &fakeResolver{digest: "sha256:aaa"}
		registrar := &fakeRegistrar{}
		s := New(nil, resolver, registrar, 0)

		require.NoError(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d@sha256:aaa")))
		assert.Equal(t, 1, resolver.calls)
		assert.Equal(t, 0, registrar.calls)
	})

	t.Run("digest changed — re-register", func(t *testing.T) {
		resolver := &fakeResolver{digest: "sha256:bbb"}
		registrar := &fakeRegistrar{}
		s := New(nil, resolver, registrar, 0)

		require.NoError(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d@sha256:aaa")))
		assert.Equal(t, 1, registrar.calls)
	})

	t.Run("pinned image ref — skipped", func(t *testing.T) {
		resolver := &fakeResolver{}
		s := New(nil, resolver, &fakeRegistrar{}, 0)

		require.NoError(t, s.syncDag(ctx, dag("reg/d@sha256:aaa", "reg/d@sha256:aaa")))
		assert.Equal(t, 0, resolver.calls)
	})

	t.Run("unpinned registration — skipped", func(t *testing.T) {
		resolver := &fakeResolver{}
		s := New(nil, resolver, &fakeRegistrar{}, 0)

		require.NoError(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d:latest")))
		assert.Equal(t, 0, resolver.calls)
	})

	t.Run("resolver error — propagated, no re-register", func(t *testing.T) {
		resolver := &fakeResolver{err: fmt.Errorf("boom")}
		registrar := &fakeRegistrar{}
		s := New(nil, resolver, registrar, 0)

		require.Error(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d@sha256:aaa")))
		assert.Equal(t, 0, registrar.calls)
	})

	t.Run("broken new image — register error propagated", func(t *testing.T) {
		resolver := &fakeResolver{digest: "sha256:bbb"}
		registrar := &fakeRegistrar{err: fmt.Errorf("invalid manifest")}
		s := New(nil, resolver, registrar, 0)

		require.Error(t, s.syncDag(ctx, dag("reg/d:latest", "reg/d@sha256:aaa")))
	})
}
