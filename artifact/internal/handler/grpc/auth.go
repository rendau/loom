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

// authorizer проверяет attempt-токены (решение №8): запись — только в свой
// attempt, чтение — в своём ране, admin-токен control plane — без
// ограничений. nil authorizer (пустой AUTH_SECRET) пропускает всё — dev-режим
// и локальные прогоны.
type authorizer struct {
	secret []byte
}

func newAuthorizer(secret string) *authorizer {
	if secret == "" {
		return nil
	}
	return &authorizer{secret: []byte(secret)}
}

func (a *authorizer) claims(ctx context.Context) (attempttoken.Claims, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get(attempttoken.MetadataKey)
	if len(values) == 0 {
		return attempttoken.Claims{}, status.Error(codes.Unauthenticated, "missing token")
	}

	claims, err := attempttoken.Verify(a.secret, values[0], time.Now())
	switch {
	case errors.Is(err, attempttoken.ErrExpired):
		return attempttoken.Claims{}, status.Error(codes.Unauthenticated, "token expired")
	case err != nil:
		return attempttoken.Claims{}, status.Error(codes.Unauthenticated, "invalid token")
	}

	return claims, nil
}

// checkAttempt — операция над конкретной попыткой (запись, abort,
// finish): admin или ровно свой attempt.
func (a *authorizer) checkAttempt(ctx context.Context, runId, task string, attempt int32) error {
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

// checkRun — чтение артефактов рана: admin или любой таск этого рана
// (таски читают выходы своих зависимостей).
func (a *authorizer) checkRun(ctx context.Context, runId string) error {
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

// checkAdmin — служебные операции control plane (retention).
func (a *authorizer) checkAdmin(ctx context.Context) error {
	if a == nil {
		return nil
	}

	c, err := a.claims(ctx)
	if err != nil {
		return err
	}
	if c.Admin {
		return nil
	}
	return status.Error(codes.PermissionDenied, "admin token required")
}
