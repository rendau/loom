package pool

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/pool/model"
)

type ServiceI interface {
	List(ctx context.Context) ([]*model.Main, error)
	Set(ctx context.Context, name string, slots int) error
}
