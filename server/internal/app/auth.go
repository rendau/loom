package app

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonPb "github.com/rendau/loom/api/common"
	"github.com/rendau/loom/server/internal/errs"
)

// Админские RPC защищаются статическим bearer-токеном (ADMIN_TOKEN).
// Task-facing ручки исключены — Push/PullTaskValue открыты внутри кластера,
// PushDagManifest — одноразовый describe_id. Новые RPC защищены по
// умолчанию.
var authExemptMethods = map[string]struct{}{
	"/server_v1.DagService/PushDagManifest":     {},
	"/server_v1.TaskValueService/PushTaskValue": {},
	"/server_v1.TaskValueService/PullTaskValue": {},
}

func authRequired(fullMethod string) bool {
	// reflection остаётся открытым: это дискавери сервисов, не данные
	if strings.HasPrefix(fullMethod, "/grpc.reflection.") {
		return false
	}
	_, exempt := authExemptMethods[fullMethod]
	return !exempt
}

// checkAdminToken сверяет metadata authorization (`Bearer <token>`; gateway
// пробрасывает HTTP-заголовок Authorization автоматически).
func checkAdminToken(ctx context.Context, token string) error {
	md, _ := metadata.FromIncomingContext(ctx)
	const prefix = "bearer "
	for _, v := range md.Get("authorization") {
		if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) &&
			subtle.ConstantTimeCompare([]byte(v[len(prefix):]), []byte(token)) == 1 {
			return nil
		}
	}

	st := status.New(codes.Unauthenticated, "admin token required")
	if detailed, err := st.WithDetails(&commonPb.ErrorRep{
		Code:    errs.NotAuthorized.Error(),
		Message: "admin token required",
	}); err == nil {
		st = detailed
	}
	return st.Err()
}

func GrpcInterceptorAuth(token string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if authRequired(info.FullMethod) {
			if err := checkAdminToken(ctx, token); err != nil {
				return nil, err
			}
		}
		return handler(ctx, req)
	}
}

func GrpcStreamInterceptorAuth(token string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if authRequired(info.FullMethod) {
			if err := checkAdminToken(ss.Context(), token); err != nil {
				return err
			}
		}
		return handler(srv, ss)
	}
}
