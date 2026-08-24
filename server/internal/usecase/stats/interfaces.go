package stats

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/stats/model"
)

type ServiceI interface {
	Dashboard(ctx context.Context) (*model.Dashboard, error)
}
