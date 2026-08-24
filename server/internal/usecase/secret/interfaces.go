package secret

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/secret/model"
)

type ServiceI interface {
	List(ctx context.Context, dagName *string) ([]*model.Meta, error)
	Set(ctx context.Context, dagName, name string, value []byte) error
	Delete(ctx context.Context, dagName, name string) error
	GetValue(ctx context.Context, dagName, name string) ([]byte, error)
}

// AuthzI — права на скоуп: глобальный доступен только admin, локальный —
// владельцу дага.
type AuthzI interface {
	RequireScope(ctx context.Context, dagName string) error
}
