package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"

	commonPb "github.com/rendau/loom/api/common"
	pb "github.com/rendau/loom/api/server_v1"
	statsModel "github.com/rendau/loom/server/internal/domain/stats/model"
	"github.com/rendau/loom/server/internal/handler/grpc/dto"
	dagUsc "github.com/rendau/loom/server/internal/usecase/dag"
)

type Dag struct {
	pb.UnsafeDagServiceServer

	usecase *dagUsc.Usecase
}

func NewDag(uc *dagUsc.Usecase) *Dag {
	return &Dag{usecase: uc}
}

func (h *Dag) RegisterDag(ctx context.Context, req *pb.DagRegisterReq) (*pb.DagRegisterRep, error) {
	reg, err := h.usecase.Register(ctx, dto.DecodeDagRegisterReq(req))
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.DagRegisterRep{RegistrationId: reg.Id}, nil
}

func (h *Dag) ListDagRegistration(ctx context.Context, req *pb.DagRegistrationListReq) (*pb.DagRegistrationListRep, error) {
	items, err := h.usecase.ListRegistrations(ctx, dto.DecodeDagRegistrationListReq(req))
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.DagRegistrationListRep{Results: lo.Map(items, dto.EncodeDagRegistrationMain)}, nil
}

func (h *Dag) GetDagRegistration(ctx context.Context, req *pb.DagRegistrationGetReq) (*pb.DagRegistrationMain, error) {
	item, err := h.usecase.GetRegistration(ctx, req.GetId())
	if err != nil {
		return nil, encodeErr(err)
	}
	return dto.EncodeDagRegistrationMain(item, 0), nil
}

func (h *Dag) ListDag(ctx context.Context, req *pb.DagListReq) (*pb.DagListRep, error) {
	if req.ListParams == nil {
		req.ListParams = &commonPb.ListParamsSt{}
	}

	items, tCount, err := h.usecase.List(ctx, dto.DecodeDagListReq(req))
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.DagListRep{
		PaginationInfo: &commonPb.PaginationInfoSt{
			Page:       req.ListParams.Page,
			PageSize:   req.ListParams.PageSize,
			TotalCount: tCount,
		},
		Results: lo.Map(items, dto.EncodeDagMain),
	}, nil
}

func (h *Dag) GetDag(ctx context.Context, req *pb.DagGetReq) (*pb.DagMain, error) {
	item, err := h.usecase.Get(ctx, req.GetName())
	if err != nil {
		return nil, encodeErr(err)
	}
	return dto.EncodeDagMain(item, 0), nil
}

func (h *Dag) GetDagStats(ctx context.Context, req *pb.DagStatsReq) (*pb.DagStatsRep, error) {
	runs, stats, err := h.usecase.GetStats(ctx, req.GetName(), int64(req.GetLastRuns()))
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.DagStatsRep{
		Runs: runs,
		Tasks: lo.Map(stats, func(s statsModel.TaskStat, _ int) *pb.DagTaskStat {
			return &pb.DagTaskStat{
				Task:               s.Task,
				Runs:               s.Runs,
				AvgDurationSec:     s.AvgDurationSec,
				MaxDurationSec:     s.MaxDurationSec,
				AvgPeakMemoryBytes: s.AvgPeakMemoryBytes,
				MaxPeakMemoryBytes: s.MaxPeakMemoryBytes,
			}
		}),
	}, nil
}

func (h *Dag) SetDagSchedule(ctx context.Context, req *pb.DagSetScheduleReq) (*emptypb.Empty, error) {
	if err := h.usecase.SetSchedule(ctx, req.GetName(), req.GetSchedule(), req.GetCatchup()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Dag) SetDagPaused(ctx context.Context, req *pb.DagSetPausedReq) (*emptypb.Empty, error) {
	if err := h.usecase.SetPaused(ctx, req.GetName(), req.GetPaused()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Dag) SyncDag(ctx context.Context, req *pb.DagSyncReq) (*pb.DagSyncRep, error) {
	reg, err := h.usecase.Sync(ctx, req.GetName())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.DagSyncRep{RegistrationId: reg.Id}, nil
}

func (h *Dag) SetDagAutoUpdate(ctx context.Context, req *pb.DagSetAutoUpdateReq) (*emptypb.Empty, error) {
	if err := h.usecase.SetAutoUpdate(ctx, req.GetName(), req.GetAutoUpdate()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Dag) DeleteDag(ctx context.Context, req *pb.DagDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, req.GetName()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

// PushDagManifest — внутренний вызов describe-Job'а; вместо
// токена его авторизует одноразовый непубличный describe_id.
func (h *Dag) PushDagManifest(ctx context.Context, req *pb.DagPushManifestReq) (*emptypb.Empty, error) {
	if err := h.usecase.PushManifest(ctx, req.GetDescribeId(), req.GetManifest(), req.GetError()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}
