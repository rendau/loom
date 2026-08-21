package handler

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/rendau/loom/api/attempttoken"
)

// tokenAuth проверяет attempt-токены на приёмниках control plane (логи,
// значения тасков): пуш — только в свой attempt, чтение значений — в своём
// ране (решение №8). nil (пустой AUTH_SECRET) пропускает всё.
type tokenAuth struct {
	secret []byte
}

func newTokenAuth(secret string) *tokenAuth {
	if secret == "" {
		return nil
	}
	return &tokenAuth{secret: []byte(secret)}
}

// claims извлекает и проверяет токен из metadata запроса.
func (a *tokenAuth) claims(ctx context.Context) (attempttoken.Claims, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(attempttoken.MetadataKey)
	if len(values) == 0 {
		return attempttoken.Claims{}, status.Error(codes.Unauthenticated, "missing token")
	}

	c, err := attempttoken.Verify(a.secret, values[0], time.Now())
	switch {
	case errors.Is(err, attempttoken.ErrExpired):
		return attempttoken.Claims{}, status.Error(codes.Unauthenticated, "token expired")
	case err != nil:
		return attempttoken.Claims{}, status.Error(codes.Unauthenticated, "invalid token")
	}
	return c, nil
}

func (a *tokenAuth) checkAttempt(ctx context.Context, runId, task string, attempt int32) error {
	if a == nil {
		return nil
	}

	c, err := a.claims(ctx)
	if err != nil {
		return err
	}

	if c.Admin || (c.RunID == runId && c.Task == task && c.Attempt == attempt) {
		return nil
	}
	return status.Error(codes.PermissionDenied, "token is not scoped to this attempt")
}

// checkRun — доступ в скоупе рана: таски читают значения зависимостей
// токеном любого attempt'а этого рана.
func (a *tokenAuth) checkRun(ctx context.Context, runId string) error {
	if a == nil {
		return nil
	}

	c, err := a.claims(ctx)
	if err != nil {
		return err
	}

	if c.Admin || c.RunID == runId {
		return nil
	}
	return status.Error(codes.PermissionDenied, "token is not scoped to this run")
}
