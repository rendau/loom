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

// tokenAuth проверяет attempt-токены на лог-приёмнике: пуш лога — только в
// свой attempt (решение №8). nil (пустой AUTH_SECRET) пропускает всё.
type tokenAuth struct {
	secret []byte
}

func newTokenAuth(secret string) *tokenAuth {
	if secret == "" {
		return nil
	}
	return &tokenAuth{secret: []byte(secret)}
}

func (a *tokenAuth) checkAttempt(ctx context.Context, runId, task string, attempt int32) error {
	if a == nil {
		return nil
	}

	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(attempttoken.MetadataKey)
	if len(values) == 0 {
		return status.Error(codes.Unauthenticated, "missing token")
	}

	c, err := attempttoken.Verify(a.secret, values[0], time.Now())
	switch {
	case errors.Is(err, attempttoken.ErrExpired):
		return status.Error(codes.Unauthenticated, "token expired")
	case err != nil:
		return status.Error(codes.Unauthenticated, "invalid token")
	}

	if c.Admin || (c.RunID == runId && c.Task == task && c.Attempt == attempt) {
		return nil
	}
	return status.Error(codes.PermissionDenied, "token is not scoped to this attempt")
}
