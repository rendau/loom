// Package service — сводка дашборда: независимые SQL-агрегаты выполняются
// параллельно, чтобы главная страница открывалась за один RTT.
package service

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/rendau/loom/server/internal/domain/stats/model"
)

// activityDays — глубина графика активности.
const activityDays = 14

type Service struct {
	repoDb RepoDbI
}

func New(repoDb RepoDbI) *Service {
	return &Service{repoDb: repoDb}
}

// DagStats — агрегаты по таскам дага за последние lastRuns завершённых
// ранов («жирные таски» админки).
func (s *Service) DagStats(ctx context.Context, dagName string, lastRuns int64) (int64, []model.TaskStat, error) {
	runs, stats, err := s.repoDb.DagTaskStats(ctx, dagName, lastRuns)
	if err != nil {
		return 0, nil, fmt.Errorf("repoDb.DagTaskStats: %w", err)
	}
	return runs, stats, nil
}

func (s *Service) Dashboard(ctx context.Context) (*model.Dashboard, error) {
	result := &model.Dashboard{}
	now := time.Now()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		activeRuns, dags, pausedDags, err := s.repoDb.Counters(ctx)
		if err != nil {
			return err
		}
		result.ActiveRuns, result.DagCount, result.PausedDagCount = activeRuns, dags, pausedDags
		return nil
	})
	g.Go(func() error {
		w, err := s.repoDb.Window(ctx, now.Add(-24*time.Hour))
		result.Last24h = w
		return err
	})
	g.Go(func() error {
		w, err := s.repoDb.Window(ctx, now.AddDate(0, 0, -7))
		result.Last7d = w
		return err
	})
	g.Go(func() error {
		items, err := s.repoDb.Upcoming(ctx)
		result.Upcoming = items
		return err
	})
	g.Go(func() error {
		items, err := s.repoDb.Pools(ctx)
		result.Pools = items
		return err
	})
	g.Go(func() error {
		items, err := s.repoDb.RecentFailures(ctx)
		result.RecentFailures = items
		return err
	})
	g.Go(func() error {
		items, err := s.repoDb.Activity(ctx, activityDays)
		result.Activity = items
		return err
	})
	g.Go(func() error {
		items, err := s.repoDb.DagDurations(ctx, now.AddDate(0, 0, -7))
		result.DagDurations = items
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("dashboard: %w", err)
	}
	return result, nil
}
