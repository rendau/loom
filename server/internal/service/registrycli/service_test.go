package registrycli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRef(t *testing.T) {
	cases := []struct {
		in                      string
		host, repo, tag, digest string
	}{
		{"127.0.0.1:5001/demo-etl:e2e", "127.0.0.1:5001", "demo-etl", "e2e", ""},
		{"localhost/repo", "localhost", "repo", "latest", ""},
		{"ghcr.io/org/dag:latest", "ghcr.io", "org/dag", "latest", ""},
		{"redis", "registry-1.docker.io", "library/redis", "latest", ""},
		{"mechta/dag:v1", "registry-1.docker.io", "mechta/dag", "v1", ""},
		{"docker.io/redis:7", "registry-1.docker.io", "library/redis", "7", ""},
		{"ghcr.io/org/dag@sha256:abc", "", "", "", "sha256:abc"},
	}
	for _, c := range cases {
		ref, err := parseRef(c.in)
		require.NoError(t, err, c.in)
		if c.digest != "" {
			assert.Equal(t, c.digest, ref.digest, c.in)
			continue
		}
		assert.Equal(t, c.host, ref.host, c.in)
		assert.Equal(t, c.repo, ref.repo, c.in)
		assert.Equal(t, c.tag, ref.tag, c.in)
	}

	_, err := parseRef("")
	require.Error(t, err)
}

// testHost переписывает host ref'а на адрес httptest-сервера (localhost →
// http, как у настоящего локального registry).
func testHost(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return u.Host
}

func TestResolveDigestAnonymous(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodHead, r.Method)
		require.Equal(t, "/v2/demo/manifests/e2e", r.URL.Path)
		w.Header().Set("Docker-Content-Digest", "sha256:aaa")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	digest, err := New("").ResolveDigest(context.Background(), testHost(t, srv)+"/demo:e2e")
	require.NoError(t, err)
	assert.Equal(t, "sha256:aaa", digest)
}

func TestResolveDigestGetFallback(t *testing.T) {
	manifest := []byte(`{"schemaVersion":2}`)
	sum := sha256.Sum256(manifest)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// заголовок digest не отдаём вовсе — клиент должен посчитать по телу
		if r.Method == http.MethodGet {
			_, _ = w.Write(manifest)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	digest, err := New("").ResolveDigest(context.Background(), testHost(t, srv)+"/demo:e2e")
	require.NoError(t, err)
	assert.Equal(t, "sha256:"+hex.EncodeToString(sum[:]), digest)
}

func TestResolveDigestTokenFlow(t *testing.T) {
	var srvURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "registry", r.URL.Query().Get("service"))
		assert.Equal(t, "repository:org/demo:pull", r.URL.Query().Get("scope"))
		// Basic-креды из config.json должны дойти до token endpoint
		user, pass, ok := r.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "bot", user)
		assert.Equal(t, "s3cret", pass)
		_, _ = fmt.Fprint(w, `{"token":"tok123"}`)
	})
	mux.HandleFunc("/v2/org/demo/manifests/v1", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok123" {
			w.Header().Set("WWW-Authenticate",
				fmt.Sprintf(`Bearer realm=%q,service="registry",scope="repository:org/demo:pull"`, srvURL+"/token"))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:bbb")
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL
	host := testHost(t, srv)

	authFile := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(authFile, fmt.Appendf(nil,
		`{"auths": {%q: {"username": "bot", "password": "s3cret"}}}`, host), 0o600))

	digest, err := New(authFile).ResolveDigest(context.Background(), host+"/org/demo:v1")
	require.NoError(t, err)
	assert.Equal(t, "sha256:bbb", digest)
}

func TestResolveDigestPinnedRef(t *testing.T) {
	digest, err := New("").ResolveDigest(context.Background(), "ghcr.io/org/dag@sha256:ccc")
	require.NoError(t, err)
	assert.Equal(t, "sha256:ccc", digest)
}

func TestResolveDigestHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := New("").ResolveDigest(context.Background(), testHost(t, srv)+"/demo:gone")
	require.ErrorContains(t, err, "HTTP 404")
}
