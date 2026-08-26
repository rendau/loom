package variable

import (
	"context"
	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/domain/variable/model"
)

type ServiceI interface {
	List(ctx context.Context, scope *commonModel.Scope) ([]*model.Main, error)
	Set(ctx context.Context, scope commonModel.Scope, name, value string) error
	Delete(ctx context.Context, scope commonModel.Scope, name string) error
	Move(ctx context.Context, from, to commonModel.Scope, name string) error
}

// AuthzI — права на скоуп: глобальный доступен только admin, локальный —
// владельцу дага.
type AuthzI interface {
	RequireScope(ctx context.Context, scope commonModel.Scope) error
}
