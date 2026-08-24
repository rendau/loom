package service

import (
	"context"
	"time"

	"github.com/rendau/loom/server/internal/domain/stats/model"
)

type RepoDbI interface {
	Counters(ctx context.Context) (activeRuns, dags, pausedDags int64, err error)
	Window(ctx context.Context, since time.Time) (model.Window, error)
	Upcoming(ctx context.Context) ([]model.Upcoming, error)
	Pools(ctx context.Context) ([]model.PoolUsage, error)
	RecentFailures(ctx context.Context) ([]model.Failure, error)
	Activity(ctx context.Context, days int) ([]model.Day, error)
	DagDurations(ctx context.Context, since time.Time) ([]model.DagDuration, error)
}
