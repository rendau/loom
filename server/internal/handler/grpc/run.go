package handler

import (
	"context"
	"time"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"

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
	params, err := dto.DecodeRunParams(req.GetParams())
	if err != nil {
		return nil, encodeErr(err)
	}

	runId, err := h.usecase.Trigger(ctx, dto.DecodeDagRef(req.GetProject(), req.GetDagName()), params)
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

func (h *Run) BackfillRun(ctx context.Context, req *pb.RunBackfillReq) (*pb.RunBackfillRep, error) {
	params, err := dto.DecodeRunParams(req.GetParams())
	if err != nil {
		return nil, encodeErr(err)
	}

	// nil-timestamp через AsTime даёт epoch, а не zero — конвертируем явно
	var from, to time.Time
	if req.GetFrom() != nil {
		from = req.GetFrom().AsTime()
	}
	if req.GetTo() != nil {
		to = req.GetTo().AsTime()
	}

	runIds, err := h.usecase.Backfill(ctx, dto.DecodeDagRef(req.GetProject(), req.GetDagName()), from, to, params)
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.RunBackfillRep{RunIds: runIds}, nil
}

func (h *Run) RetryTask(ctx context.Context, req *pb.RunRetryTaskReq) (*emptypb.Empty, error) {
	if err := h.usecase.RetryTask(ctx, req.GetRunId(), req.GetTask()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Run) CancelRun(ctx context.Context, req *pb.RunCancelReq) (*emptypb.Empty, error) {
	if err := h.usecase.Cancel(ctx, req.GetId()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Run) GetRun(ctx context.Context, req *pb.RunGetReq) (*pb.RunGetRep, error) {
	run, manifestTasks, tasks, attempts, env, err := h.usecase.Get(ctx, req.GetId())
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.RunGetRep{
		Run:           dto.EncodeRunMain(run, 0),
		Tasks:         lo.Map(tasks, dto.EncodeTaskInstanceMain),
		Attempts:      lo.Map(attempts, dto.EncodeAttemptMain),
		ManifestTasks: lo.Map(manifestTasks, dto.EncodeDagTaskMain),
		Env:           lo.Map(env, dto.EncodeRunEnvMain),
	}, nil
}

func (h *Run) CountRun(ctx context.Context, req *pb.RunCountReq) (*pb.RunCountRep, error) {
	counts, err := h.usecase.Count(ctx, dto.DecodeDagRefFilter(req.Project, req.DagName))
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.RunCountRep{
		Running:  counts["running"],
		Success:  counts["success"],
		Failed:   counts["failed"],
		Canceled: counts["canceled"],
	}, nil
}
