// Package authz — проверки прав на ресурс (даг) для usecase-слоя. Роль
// метода проверяет auth-интерцептор; здесь — то, что зависит от данных
// запроса: имя дага известно только в usecase.
package authz

import (
	"context"
	"fmt"

	"github.com/rendau/loom/server/internal/authctx"
	userModel "github.com/rendau/loom/server/internal/domain/user/model"
	"github.com/rendau/loom/server/internal/errs"
)

// DagAccessI — источник прав: реализует user-сервис.
type DagAccessI interface {
	CanManageDag(ctx context.Context, info userModel.AuthInfo, dagName string) (bool, error)
}

type Checker struct {
	access DagAccessI
}

func New(access DagAccessI) *Checker {
	return &Checker{access: access}
}

// RequireDag разрешает менять даг: admin — любой, обычный пользователь —
// только назначенный ему. Вызов без аутентификации (внутрикластерные RPC,
// фоновые процессы) не ограничивается.
func (c *Checker) RequireDag(ctx context.Context, dagName string) error {
	info, ok := authctx.Info(ctx)
	if !ok {
		return nil
	}
	allowed, err := c.access.CanManageDag(ctx, info, dagName)
	if err != nil {
		return fmt.Errorf("access.CanManageDag: %w", err)
	}
	if !allowed {
		return errs.ErrFull{Err: errs.PermissionDenied,
			Desc: fmt.Sprintf("нет прав на даг %q", dagName)}
	}
	return nil
}

// RequireAdmin разрешает операции вне скоупа дага (глобальные переменные и
// секреты, управление пулами).
func (c *Checker) RequireAdmin(ctx context.Context) error {
	if authctx.IsAdmin(ctx) {
		return nil
	}
	return errs.ErrFull{Err: errs.PermissionDenied, Desc: "требуются права администратора"}
}

// RequireScope — права на скоуп переменной/секрета: пустой dagName —
// глобальный (только admin), иначе — права на этот даг.
func (c *Checker) RequireScope(ctx context.Context, dagName string) error {
	if dagName == "" {
		return c.RequireAdmin(ctx)
	}
	return c.RequireDag(ctx, dagName)
}
