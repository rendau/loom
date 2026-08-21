package secret

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/secret/model"
)

type ServiceI interface {
	List(ctx context.Context) ([]*model.Meta, error)
	Set(ctx context.Context, name string, value []byte) error
	Delete(ctx context.Context, name string) error
}
