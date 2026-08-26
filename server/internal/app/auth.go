package app

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	commonPb "github.com/rendau/loom/api/common"
	"github.com/rendau/loom/server/internal/authctx"
	userModel "github.com/rendau/loom/server/internal/domain/user/model"
	"github.com/rendau/loom/server/internal/errs"
)

// Админские RPC требуют сессии пользователя (`Authorization: Bearer
// <token>` — токен выдаёт AuthService.Login). Исключения:
//   - task-facing ручки: Push/PullTaskValue открыты внутри кластера,
//     PushDagCatalog — одноразовый describe_id;
//   - вход и первичная настройка: без них залогиниться было бы нельзя.
//
// Новые RPC защищены по умолчанию. Имена сверяются с дескрипторами proto
// в тесте: переименованный метод иначе тихо теряет исключение (и
// describe-Job перестаёт доставлять каталог).
var authExemptMethods = map[string]struct{}{
	"/server_v1.ProjectService/PushDagCatalog":  {},
	"/server_v1.TaskValueService/PushTaskValue": {},
	"/server_v1.TaskValueService/PullTaskValue": {},
	"/server_v1.AuthService/GetAuthStatus":      {},
	"/server_v1.AuthService/CreateFirstAdmin":   {},
	"/server_v1.AuthService/Login":              {},
}

// adminOnlyMethods — операции уровня инсталляции: удаление проекта со
// всеми его дагами, удаление дага, пулы, управление пользователями. Права
// на конкретный проект или даг (регистрация образа, расписание, триггер,
// переменные) проверяются в usecase — интерцептору имя дага не видно.
var adminOnlyMethods = map[string]struct{}{
	"/server_v1.ProjectService/DeleteProject": {},
	"/server_v1.DagService/DeleteDag":         {},
	"/server_v1.PoolService/SetPool":          {},
}

// AuthenticatorI — проверка токена сессии (реализует user-сервис).
type AuthenticatorI interface {
	Authenticate(ctx context.Context, token string) (userModel.AuthInfo, error)
}

func authRequired(fullMethod string) bool {
	// reflection остаётся открытым: это дискавери сервисов, не данные
	if strings.HasPrefix(fullMethod, "/grpc.reflection.") {
		return false
	}
	_, exempt := authExemptMethods[fullMethod]
	return !exempt
}

func adminRequired(fullMethod string) bool {
	if strings.HasPrefix(fullMethod, "/server_v1.UserService/") {
		return true
	}
	_, ok := adminOnlyMethods[fullMethod]
	return ok
}

// BearerToken достаёт токен из metadata (gateway пробрасывает HTTP-заголовок
// Authorization автоматически).
func BearerToken(ctx context.Context) string {
	md, _ := metadata.FromIncomingContext(ctx)
	const prefix = "bearer "
	for _, v := range md.Get("authorization") {
		if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
			return v[len(prefix):]
		}
	}
	return ""
}

// authenticate проверяет сессию и кладёт вызывающего в контекст.
func authenticate(ctx context.Context, auth AuthenticatorI, fullMethod string) (context.Context, error) {
	token := BearerToken(ctx)
	if token == "" {
		return nil, authErr(codes.Unauthenticated, errs.NotAuthorized, "требуется вход")
	}

	info, err := auth.Authenticate(ctx, token)
	if err != nil {
		return nil, authErr(codes.Unauthenticated, errs.NotAuthorized, "сессия недействительна")
	}
	if adminRequired(fullMethod) && !info.IsAdmin() {
		return nil, authErr(codes.PermissionDenied, errs.PermissionDenied, "требуются права администратора")
	}
	return authctx.With(ctx, info), nil
}

func authErr(code codes.Code, errCode errs.Err, message string) error {
	st := status.New(code, message)
	if detailed, err := st.WithDetails(&commonPb.ErrorRep{
		Code:    errCode.Error(),
		Message: message,
	}); err == nil {
		st = detailed
	}
	return st.Err()
}

func GrpcInterceptorAuth(auth AuthenticatorI) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if authRequired(info.FullMethod) {
			authedCtx, err := authenticate(ctx, auth, info.FullMethod)
			if err != nil {
				return nil, err
			}
			ctx = authedCtx
		}
		return handler(ctx, req)
	}
}

func GrpcStreamInterceptorAuth(auth AuthenticatorI) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !authRequired(info.FullMethod) {
			return handler(srv, ss)
		}

		authedCtx, err := authenticate(ss.Context(), auth, info.FullMethod)
		if err != nil {
			return err
		}
		return handler(srv, &wrappedStream{ServerStream: ss, ctx: authedCtx})
	}
}

// wrappedStream подменяет контекст стрима на аутентифицированный.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
