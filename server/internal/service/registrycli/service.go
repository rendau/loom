// Package registrycli — лёгкий клиент Docker Registry HTTP API v2 для
// авто-обновления дагов: резолв текущего digest'а тега без
// скачивания образа (HEAD /v2/<repo>/manifests/<tag>). Поддерживает
// anonymous-доступ, Basic и token-auth (Bearer challenge, как у ghcr и
// Docker Hub); креды — стандартный docker config.json (путь в
// REGISTRY_AUTH_FILE). localhost-registry опрашивается по http (как в
// docker: локальный registry — insecure).
package registrycli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	requestTimeout = 15 * time.Second

	// maxBodySize — предохранитель на тело манифеста (GET-fallback, когда
	// registry не отдаёт Docker-Content-Digest на HEAD).
	maxBodySize = 4 << 20

	acceptManifests = "application/vnd.docker.distribution.manifest.v2+json, " +
		"application/vnd.docker.distribution.manifest.list.v2+json, " +
		"application/vnd.oci.image.manifest.v1+json, " +
		"application/vnd.oci.image.index.v1+json"
)

type Service struct {
	authFile string // путь к docker config.json; пусто — только anonymous
	client   *http.Client
}

func New(authFile string) *Service {
	return &Service{
		authFile: authFile,
		client:   &http.Client{Timeout: requestTimeout},
	}
}

// ResolveDigest возвращает текущий digest тега образа («sha256:...») по
// registry API. Ref, уже пиннутый digest'ом, возвращается как есть.
func (s *Service) ResolveDigest(ctx context.Context, image string) (string, error) {
	ref, err := parseRef(image)
	if err != nil {
		return "", err
	}
	if ref.digest != "" {
		return ref.digest, nil
	}

	manifestUrl := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", ref.scheme(), ref.host, ref.repo, ref.tag)

	digest, err := s.fetchDigest(ctx, "HEAD", manifestUrl, ref, "")
	if err != nil {
		return "", err
	}
	if digest == "" {
		// registry не отдал заголовок на HEAD — digest считается по телу
		// манифеста (digest и есть sha256 канонического тела)
		digest, err = s.fetchDigest(ctx, "GET", manifestUrl, ref, "")
		if err != nil {
			return "", err
		}
	}
	if digest == "" {
		return "", fmt.Errorf("registry %s: no digest for %s:%s", ref.host, ref.repo, ref.tag)
	}
	return digest, nil
}

// fetchDigest делает запрос манифеста, при 401 проходит auth-challenge и
// повторяет. Для GET при отсутствии заголовка digest считается по телу.
func (s *Service) fetchDigest(ctx context.Context, method, manifestUrl string, ref imageRef, authHeader string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, method, manifestUrl, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", acceptManifests)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("registry request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && authHeader == "" {
		header, authErr := s.authorize(ctx, resp.Header.Get("WWW-Authenticate"), ref)
		if authErr != nil {
			return "", authErr
		}
		return s.fetchDigest(ctx, method, manifestUrl, ref, header)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry %s: %s %s:%s: HTTP %d", ref.host, method, ref.repo, ref.tag, resp.StatusCode)
	}

	if digest := resp.Header.Get("Docker-Content-Digest"); digest != "" {
		return digest, nil
	}
	if method != http.MethodGet {
		return "", nil // вызывающий повторит GET'ом
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return "", fmt.Errorf("read manifest: %w", err)
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// authorize превращает 401-challenge в Authorization-заголовок: Bearer —
// поход за токеном (с Basic-кредами, если настроены), Basic — креды напрямую.
func (s *Service) authorize(ctx context.Context, challenge string, ref imageRef) (string, error) {
	scheme, params, _ := strings.Cut(challenge, " ")

	switch strings.ToLower(scheme) {
	case "bearer":
		return s.fetchToken(ctx, parseChallengeParams(params), ref)
	case "basic":
		creds := s.credsFor(ref.host)
		if creds == "" {
			return "", fmt.Errorf("registry %s requires basic auth, no credentials configured", ref.host)
		}
		return "Basic " + creds, nil
	default:
		return "", fmt.Errorf("registry %s: unsupported auth challenge %q", ref.host, challenge)
	}
}

// fetchToken — token-flow Docker Registry: GET realm?service=...&scope=
// repository:<repo>:pull, опционально с Basic-кредами.
func (s *Service) fetchToken(ctx context.Context, challenge map[string]string, ref imageRef) (string, error) {
	realm := challenge["realm"]
	if realm == "" {
		return "", fmt.Errorf("registry %s: bearer challenge without realm", ref.host)
	}

	q := url.Values{}
	if v := challenge["service"]; v != "" {
		q.Set("service", v)
	}
	if v := challenge["scope"]; v != "" {
		q.Set("scope", v)
	} else {
		q.Set("scope", "repository:"+ref.repo+":pull")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, realm+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	if creds := s.credsFor(ref.host); creds != "" {
		req.Header.Set("Authorization", "Basic "+creds)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry %s: token endpoint HTTP %d", ref.host, resp.StatusCode)
	}

	var rep struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, maxBodySize)).Decode(&rep); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	token := rep.Token
	if token == "" {
		token = rep.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("registry %s: empty token", ref.host)
	}
	return "Bearer " + token, nil
}

// credsFor возвращает base64(user:pass) для хоста из docker config.json;
// пусто — кредов нет (anonymous).
func (s *Service) credsFor(host string) string {
	if s.authFile == "" {
		return ""
	}

	raw, err := os.ReadFile(s.authFile)
	if err != nil {
		return ""
	}

	var conf struct {
		Auths map[string]struct {
			Auth     string `json:"auth"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"auths"`
	}
	if err = json.Unmarshal(raw, &conf); err != nil {
		return ""
	}

	keys := []string{host}
	if host == defaultRegistryHost || host == "docker.io" {
		keys = append(keys, "https://index.docker.io/v1/", "index.docker.io", "docker.io")
	}
	for _, key := range keys {
		entry, ok := conf.Auths[key]
		if !ok {
			continue
		}
		if entry.Auth != "" {
			return entry.Auth
		}
		if entry.Username != "" {
			return base64.StdEncoding.EncodeToString([]byte(entry.Username + ":" + entry.Password))
		}
	}
	return ""
}

// parseChallengeParams разбирает параметры challenge:
// realm="https://...",service="registry",scope="...".
func parseChallengeParams(s string) map[string]string {
	result := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		result[strings.ToLower(key)] = strings.Trim(value, `"`)
	}
	return result
}

// ── разбор image ref ────────────────────────────────────

const defaultRegistryHost = "registry-1.docker.io"

type imageRef struct {
	host   string
	repo   string
	tag    string
	digest string // непустой — ref уже пиннут digest'ом
}

// scheme — http для локального registry (как docker относится к localhost),
// https для остальных.
func (r imageRef) scheme() string {
	hostname := r.host
	if h, _, err := net.SplitHostPort(r.host); err == nil {
		hostname = h
	}
	switch hostname {
	case "localhost", "127.0.0.1", "::1":
		return "http"
	}
	return "https"
}

// parseRef разбирает docker image ref по правилам docker: хост присутствует,
// только если первый сегмент содержит точку/порт или равен localhost; без
// хоста — Docker Hub (короткие имена получают префикс library/).
func parseRef(image string) (imageRef, error) {
	if image == "" {
		return imageRef{}, fmt.Errorf("empty image ref")
	}

	ref := imageRef{tag: "latest"}

	rest := image
	if rest, ref.digest, _ = strings.Cut(rest, "@"); ref.digest != "" && !strings.HasPrefix(ref.digest, "sha256:") {
		return imageRef{}, fmt.Errorf("unsupported digest in ref %q", image)
	}

	first, remainder, hasSlash := strings.Cut(rest, "/")
	if hasSlash && (strings.ContainsAny(first, ".:") || first == "localhost") {
		ref.host = first
		rest = remainder
	}

	// тег — после последнего ":", если он правее последнего "/"
	if idx := strings.LastIndex(rest, ":"); idx > strings.LastIndex(rest, "/") {
		ref.repo, ref.tag = rest[:idx], rest[idx+1:]
	} else {
		ref.repo = rest
	}
	if ref.repo == "" {
		return imageRef{}, fmt.Errorf("invalid image ref %q", image)
	}

	if ref.host == "" || ref.host == "docker.io" {
		ref.host = defaultRegistryHost
		if !strings.Contains(ref.repo, "/") {
			ref.repo = "library/" + ref.repo
		}
	}

	return ref, nil
}
