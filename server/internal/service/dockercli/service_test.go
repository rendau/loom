package dockercli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubDocker кладёт исполняемый скрипт-заглушку docker и возвращает сервис
// поверх него.
func stubDocker(t *testing.T, script string) *Service {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "docker")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"+script), 0o755))

	return New(bin)
}

func TestInspect(t *testing.T) {
	svc := stubDocker(t, `
case "$1" in
  pull) echo "pulled $2" ;;
  image) echo "registry/demo@sha256:abc" ;;
  run) echo '{"sdk_version":"0.1.0","name":"demo","tasks":[{"name":"a"}]}' ;;
  *) echo "unexpected: $@" >&2; exit 1 ;;
esac`)

	digest, raw, err := svc.Inspect(context.Background(), "registry/demo:latest")
	require.NoError(t, err)
	assert.Equal(t, "registry/demo@sha256:abc", digest)
	assert.Contains(t, string(raw), `"name":"demo"`)
}

func TestResolveDigestFallbackToImage(t *testing.T) {
	// локально собранный образ без RepoDigests — пиннинга нет, возвращаем
	// исходный ref
	svc := stubDocker(t, `echo ""`)

	digest, err := svc.resolveDigest(context.Background(), "local/demo:dev")
	require.NoError(t, err)
	assert.Equal(t, "local/demo:dev", digest)
}

func TestErrorIncludesStderr(t *testing.T) {
	svc := stubDocker(t, `echo "manifest unknown" >&2; exit 1`)

	_, _, err := svc.Inspect(context.Background(), "registry/ghost:latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest unknown")
}
