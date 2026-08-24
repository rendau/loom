// Package authctx — аутентифицированный вызывающий в контексте запроса:
// кладёт auth-интерцептор, читают usecase'ы при проверке прав на ресурс.
package authctx

import (
	"context"

	userModel "github.com/rendau/loom/server/internal/domain/user/model"
)

type ctxKey struct{}

func With(ctx context.Context, info userModel.AuthInfo) context.Context {
	return context.WithValue(ctx, ctxKey{}, info)
}

// Info возвращает вызывающего; false — вызов без аутентификации
// (внутрикластерные RPC, фоновые процессы control plane).
func Info(ctx context.Context) (userModel.AuthInfo, bool) {
	info, ok := ctx.Value(ctxKey{}).(userModel.AuthInfo)
	return info, ok
}

// IsAdmin — true и для неаутентифицированных внутренних вызовов
// (фоновые процессы не ограничиваем ролями).
func IsAdmin(ctx context.Context) bool {
	info, ok := Info(ctx)
	return !ok || info.IsAdmin()
}
