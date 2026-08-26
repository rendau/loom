package secret

import (
	"context"
	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/domain/secret/model"
)

type ServiceI interface {
	List(ctx context.Context, scope *commonModel.Scope) ([]*model.Meta, error)
	Set(ctx context.Context, scope commonModel.Scope, name string, value []byte) error
	Delete(ctx context.Context, scope commonModel.Scope, name string) error
	GetValue(ctx context.Context, scope commonModel.Scope, name string) ([]byte, error)
}

// AuthzI — права на скоуп: глобальный доступен только admin, локальный —
// владельцу дага.
type AuthzI interface {
	RequireScope(ctx context.Context, scope commonModel.Scope) error
}
