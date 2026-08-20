package handler

import (
	"context"

	"github.com/samber/lo"

	commonPb "github.com/rendau/loom/api/common"
	pb "github.com/rendau/loom/api/server_v1"
	"github.com/rendau/loom/server/internal/handler/grpc/dto"
	runUsc "github.com/rendau/loom/server/internal/usecase/run"
)

type Run struct {
	pb.UnsafeRunServiceServer

	usecase *runUsc.Usecase
}

func NewRun(uc *runUsc.Usecase) *Run {
	return &Run{usecase: uc}
}

func (h *Run) TriggerRun(ctx context.Context, req *pb.RunTriggerReq) (*pb.RunTriggerRep, error) {
	runId, err := h.usecase.Trigger(ctx, req.GetDagName())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.RunTriggerRep{RunId: runId}, nil
}

func (h *Run) ListRun(ctx context.Context, req *pb.RunListReq) (*pb.RunListRep, error) {
	if req.ListParams == nil {
		req.ListParams = &commonPb.ListParamsSt{}
	}

	items, tCount, err := h.usecase.List(ctx, dto.DecodeRunListReq(req))
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.RunListRep{
		PaginationInfo: &commonPb.PaginationInfoSt{
			Page:       req.ListParams.Page,
			PageSize:   req.ListParams.PageSize,
			TotalCount: tCount,
		},
		Results: lo.Map(items, dto.EncodeRunMain),
	}, nil
}

func (h *Run) GetRun(ctx context.Context, req *pb.RunGetReq) (*pb.RunGetRep, error) {
	run, tasks, attempts, err := h.usecase.Get(ctx, req.GetId())
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.RunGetRep{
		Run:      dto.EncodeRunMain(run, 0),
		Tasks:    lo.Map(tasks, dto.EncodeTaskInstanceMain),
		Attempts: lo.Map(attempts, dto.EncodeAttemptMain),
	}, nil
}
