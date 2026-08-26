// Package authz — проверки прав на ресурс (проект, даг, скоуп значений)
// для usecase-слоя. Роль метода проверяет auth-интерцептор; здесь — то,
// что зависит от данных запроса: ссылка на даг известна только в usecase.
package authz

import (
	"context"
	"fmt"

	"github.com/rendau/loom/server/internal/authctx"
	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	userModel "github.com/rendau/loom/server/internal/domain/user/model"
	"github.com/rendau/loom/server/internal/errs"
)

// DagAccessI — источник прав: реализует user-сервис.
type DagAccessI interface {
	CanManageDag(ctx context.Context, info userModel.AuthInfo, ref dagModel.Ref) (bool, error)
	CanManageProject(ctx context.Context, info userModel.AuthInfo, project string) (bool, error)
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
func (c *Checker) RequireDag(ctx context.Context, ref dagModel.Ref) error {
	info, ok := authctx.Info(ctx)
	if !ok {
		return nil
	}
	allowed, err := c.access.CanManageDag(ctx, info, ref)
	if err != nil {
		return fmt.Errorf("access.CanManageDag: %w", err)
	}
	if !allowed {
		return errs.ErrFull{Err: errs.PermissionDenied,
			Desc: fmt.Sprintf("нет прав на даг %q", ref)}
	}
	return nil
}

// RequireProject разрешает менять проект целиком (регистрация образа,
// настройки проекта, его переменные и секреты): admin — любой, обычный
// пользователь — только назначенный ему.
func (c *Checker) RequireProject(ctx context.Context, project string) error {
	info, ok := authctx.Info(ctx)
	if !ok {
		return nil
	}
	allowed, err := c.access.CanManageProject(ctx, info, project)
	if err != nil {
		return fmt.Errorf("access.CanManageProject: %w", err)
	}
	if !allowed {
		return errs.ErrFull{Err: errs.PermissionDenied,
			Desc: fmt.Sprintf("нет прав на проект %q", project)}
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

// RequireScope — права на скоуп переменной, секрета или настройки:
// глобальный — только admin, проектный — права на проект, скоуп дага —
// права на этот даг.
func (c *Checker) RequireScope(ctx context.Context, scope commonModel.Scope) error {
	switch {
	case scope.IsDag():
		return c.RequireDag(ctx, dagModel.NewRef(scope.Project, scope.Dag))
	case scope.IsProject():
		return c.RequireProject(ctx, scope.Project)
	default:
		return c.RequireAdmin(ctx)
	}
}
