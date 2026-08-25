package setting

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/setting/model"
)

type ServiceI interface {
	List(ctx context.Context, dagName *string) ([]*model.Main, error)
	Set(ctx context.Context, dagName, name, value string) error
	Delete(ctx context.Context, dagName, name string) error
}

// AuthzI — права на скоуп: глобальный доступен только admin, локальный —
// владельцу дага.
type AuthzI interface {
	RequireScope(ctx context.Context, dagName string) error
}
