package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/rendau/loom/api/attempttoken"
)

const testSecret = "log-secret"

func tokenCtx(t *testing.T, claims attempttoken.Claims) context.Context {
	t.Helper()
	if claims.ExpiresAt == 0 {
		claims.ExpiresAt = time.Now().Add(time.Minute).Unix()
	}
	token, err := attempttoken.Sign([]byte(testSecret), claims)
	require.NoError(t, err)
	return metadata.NewIncomingContext(context.Background(),
		metadata.Pairs(attempttoken.MetadataKey, token))
}

func TestTokenAuthCheckAttempt(t *testing.T) {
	auth := newTokenAuth(testSecret)

	// свой attempt — ок
	ctx := tokenCtx(t, attempttoken.Claims{RunID: "r1", Task: "a", Attempt: 2})
	assert.NoError(t, auth.checkAttempt(ctx, "r1", "a", 2))

	// admin — ок
	ctx = tokenCtx(t, attempttoken.Claims{Admin: true})
	assert.NoError(t, auth.checkAttempt(ctx, "r1", "a", 2))

	// чужой attempt того же таска — отказ
	ctx = tokenCtx(t, attempttoken.Claims{RunID: "r1", Task: "a", Attempt: 1})
	assert.Equal(t, codes.PermissionDenied, status.Code(auth.checkAttempt(ctx, "r1", "a", 2)))

	// чужой ран — отказ
	ctx = tokenCtx(t, attempttoken.Claims{RunID: "r2", Task: "a", Attempt: 2})
	assert.Equal(t, codes.PermissionDenied, status.Code(auth.checkAttempt(ctx, "r1", "a", 2)))

	// без токена — Unauthenticated
	assert.Equal(t, codes.Unauthenticated, status.Code(auth.checkAttempt(context.Background(), "r1", "a", 2)))

	// просроченный — Unauthenticated
	ctx = tokenCtx(t, attempttoken.Claims{RunID: "r1", Task: "a", Attempt: 2,
		ExpiresAt: time.Now().Add(-time.Minute).Unix()})
	assert.Equal(t, codes.Unauthenticated, status.Code(auth.checkAttempt(ctx, "r1", "a", 2)))

	// выключенная авторизация (пустой секрет) пропускает всё
	var off *tokenAuth = newTokenAuth("")
	assert.Nil(t, off)
	assert.NoError(t, off.checkAttempt(context.Background(), "r1", "a", 2))
}
