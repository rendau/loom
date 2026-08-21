// Package dockercli — инспекция docker-образов дагов через container-CLI
// (docker или совместимый бинарь): pull, резолв digest, запуск `describe`
// для получения манифеста при регистрации дага. Используется при
// EXECUTOR=docker/none; в k8s — k8sdescriber (решение №29).
package dockercli

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const (
	pullTimeout     = 5 * time.Minute
	inspectTimeout  = 30 * time.Second
	describeTimeout = 2 * time.Minute

	// maxManifestSize — предохранитель на вывод `describe` чужого образа.
	maxManifestSize = 1 << 20
)

type Service struct {
	bin string
}

func New(bin string) *Service {
	return &Service{bin: bin}
}

// Inspect — регистрационная инспекция образа: pull → пиннутый digest →
// запуск контейнера в режиме `describe` → JSON-манифест дага.
func (s *Service) Inspect(ctx context.Context, image string) (string, []byte, error) {
	if err := s.pull(ctx, image); err != nil {
		return "", nil, err
	}

	digest, err := s.resolveDigest(ctx, image)
	if err != nil {
		return "", nil, err
	}

	manifest, err := s.describe(ctx, digest)
	if err != nil {
		return "", nil, err
	}
	return digest, manifest, nil
}

func (s *Service) pull(ctx context.Context, image string) error {
	ctx, cancel := context.WithTimeout(ctx, pullTimeout)
	defer cancel()

	if _, err := s.run(ctx, "pull", image); err != nil {
		return fmt.Errorf("docker pull: %w", err)
	}
	return nil
}

// resolveDigest возвращает пиннутый ref образа (repo@sha256:...) — им
// пиннятся раны. Для образа без RepoDigests (собран локально, не пушился)
// возвращает исходный ref с предупреждением: пиннинга не будет.
func (s *Service) resolveDigest(ctx context.Context, image string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, inspectTimeout)
	defer cancel()

	out, err := s.run(ctx, "image", "inspect", "--format", "{{join .RepoDigests \"\\n\"}}", image)
	if err != nil {
		return "", fmt.Errorf("docker image inspect: %w", err)
	}

	digest, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if digest == "" {
		slog.Warn("image has no repo digest, run will not be pinned", "image", image)
		return image, nil
	}
	return digest, nil
}

// describe запускает контейнер образа в режиме `describe` и возвращает
// JSON-манифест дага.
func (s *Service) describe(ctx context.Context, image string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, describeTimeout)
	defer cancel()

	out, err := s.run(ctx, "run", "--rm", image, "describe")
	if err != nil {
		return nil, fmt.Errorf("docker run describe: %w", err)
	}
	if len(out) > maxManifestSize {
		return nil, fmt.Errorf("manifest too large: %d bytes", len(out))
	}
	return out, nil
}

func (s *Service) run(ctx context.Context, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s %s: %s", s.bin, args[0], msg)
	}

	return stdout.Bytes(), nil
}
