package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	statsModel "github.com/rendau/loom/server/internal/domain/stats/model"
	statsUsc "github.com/rendau/loom/server/internal/usecase/stats"
)

type Dashboard struct {
	pb.UnsafeDashboardServiceServer

	usecase *statsUsc.Usecase
}

func NewDashboard(uc *statsUsc.Usecase) *Dashboard {
	return &Dashboard{usecase: uc}
}

func (h *Dashboard) GetDashboard(ctx context.Context, _ *emptypb.Empty) (*pb.DashboardRep, error) {
	d, err := h.usecase.Dashboard(ctx)
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.DashboardRep{
		ActiveRuns:     d.ActiveRuns,
		DagCount:       d.DagCount,
		PausedDagCount: d.PausedDagCount,
		Last_24H:       encodeWindow(d.Last24h),
		Last_7D:        encodeWindow(d.Last7d),
		Upcoming: lo.Map(d.Upcoming, func(v statsModel.Upcoming, _ int) *pb.DashboardUpcoming {
			return &pb.DashboardUpcoming{
				DagName:   v.DagName,
				NextRunAt: timestamppb.New(v.NextRunAt),
				Schedule:  v.Schedule,
			}
		}),
		Pools: lo.Map(d.Pools, func(v statsModel.PoolUsage, _ int) *pb.DashboardPool {
			return &pb.DashboardPool{Name: v.Name, Slots: v.Slots, Busy: v.Busy}
		}),
		RecentFailures: lo.Map(d.RecentFailures, func(v statsModel.Failure, _ int) *pb.DashboardFailure {
			return &pb.DashboardFailure{
				RunId:      v.RunId,
				DagName:    v.DagName,
				FinishedAt: timestamppb.New(v.FinishedAt),
			}
		}),
		Activity: lo.Map(d.Activity, func(v statsModel.Day, _ int) *pb.DashboardDay {
			return &pb.DashboardDay{Date: v.Date, Success: v.Success, Failed: v.Failed, Running: v.Running}
		}),
		DagDurations: lo.Map(d.DagDurations, func(v statsModel.DagDuration, _ int) *pb.DashboardDagDuration {
			return &pb.DashboardDagDuration{
				DagName: v.DagName, AvgSec: v.AvgSec, MaxSec: v.MaxSec, Runs: v.Runs,
			}
		}),
	}, nil
}

func encodeWindow(v statsModel.Window) *pb.DashboardWindow {
	return &pb.DashboardWindow{Success: v.Success, Failed: v.Failed}
}
