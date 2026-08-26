package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"

	"google.golang.org/protobuf/types/known/timestamppb"

	commonPb "github.com/rendau/loom/api/common"
	pb "github.com/rendau/loom/api/server_v1"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
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

func (h *Dag) CreateDag(ctx context.Context, req *pb.DagCreateReq) (*pb.DagMain, error) {
	item, err := h.usecase.Create(ctx, dagUsc.CreateSpec{
		Project:  req.GetProject(),
		Template: req.GetTemplate(),
		Name:     req.GetName(),
		Schedule: req.Schedule,
		Catchup:  req.Catchup,
		Paused:   req.Paused,
		Pool:     req.Pool,
	})
	if err != nil {
		return nil, encodeErr(err)
	}
	return dto.EncodeDagMain(item, 0), nil
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
	item, err := h.usecase.Get(ctx, dto.DecodeDagRef(req.GetProject(), req.GetName()))
	if err != nil {
		return nil, encodeErr(err)
	}
	return dto.EncodeDagMain(item, 0), nil
}

func (h *Dag) GetDagStats(ctx context.Context, req *pb.DagStatsReq) (*pb.DagStatsRep, error) {
	runs, stats, err := h.usecase.GetStats(ctx,
		dto.DecodeDagRef(req.GetProject(), req.GetName()), int64(req.GetLastRuns()))
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
	err := h.usecase.SetSchedule(ctx, dto.DecodeDagRef(req.GetProject(), req.GetName()),
		req.GetSchedule(), req.GetCatchup())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Dag) SetDagPaused(ctx context.Context, req *pb.DagSetPausedReq) (*emptypb.Empty, error) {
	err := h.usecase.SetPaused(ctx, dto.DecodeDagRef(req.GetProject(), req.GetName()), req.GetPaused())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Dag) SetDagPool(ctx context.Context, req *pb.DagSetPoolReq) (*emptypb.Empty, error) {
	err := h.usecase.SetPool(ctx, dto.DecodeDagRef(req.GetProject(), req.GetName()), req.GetPool())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Dag) DeleteDag(ctx context.Context, req *pb.DagDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, dto.DecodeDagRef(req.GetProject(), req.GetName())); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Dag) ListTaskResources(ctx context.Context, req *pb.TaskResourcesListReq) (*pb.TaskResourcesListRep, error) {
	items, err := h.usecase.ListTaskResources(ctx, dto.DecodeDagRef(req.GetProject(), req.GetName()))
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.TaskResourcesListRep{Results: lo.Map(items, encodeTaskResourcesEntry)}, nil
}

func (h *Dag) SetTaskResources(ctx context.Context, req *pb.TaskResourcesSetReq) (*emptypb.Empty, error) {
	err := h.usecase.SetTaskResources(ctx, dto.DecodeDagRef(req.GetProject(), req.GetName()),
		req.GetTask(), dagModel.TaskResources{
			CPURequest:    req.GetCpuRequest(),
			CPULimit:      req.GetCpuLimit(),
			MemoryRequest: req.GetMemoryRequest(),
			MemoryLimit:   req.GetMemoryLimit(),
		})
	if err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Dag) DeleteTaskResources(ctx context.Context, req *pb.TaskResourcesDeleteReq) (*emptypb.Empty, error) {
	err := h.usecase.DeleteTaskResources(ctx,
		dto.DecodeDagRef(req.GetProject(), req.GetName()), req.GetTask())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func encodeTaskResourcesEntry(v *dagModel.TaskResourcesEntry, _ int) *pb.TaskResourcesMain {
	return &pb.TaskResourcesMain{
		Task:          v.Task,
		CpuRequest:    v.Res.CPURequest,
		CpuLimit:      v.Res.CPULimit,
		MemoryRequest: v.Res.MemoryRequest,
		MemoryLimit:   v.Res.MemoryLimit,
		ModifiedAt:    timestamppb.New(v.ModifiedAt),
	}
}
