package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/rendau/loom/api/server_v1"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
	"github.com/rendau/loom/server/internal/handler/grpc/dto"
	runUsc "github.com/rendau/loom/server/internal/usecase/run"
)

type TaskValue struct {
	pb.UnsafeTaskValueServiceServer

	usecase *runUsc.Usecase
}

func NewTaskValue(uc *runUsc.Usecase) *TaskValue {
	return &TaskValue{usecase: uc}
}

func (h *TaskValue) PushTaskValue(ctx context.Context, req *pb.TaskValuePushReq) (*emptypb.Empty, error) {
	if req.GetValue() == nil {
		return nil, status.Error(codes.InvalidArgument, "value is required")
	}

	raw, err := req.GetValue().MarshalJSON()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid value")
	}

	ref := runModel.AttemptRef{RunId: req.GetRunId(), Task: req.GetTask(), Attempt: req.GetAttempt()}
	if err = h.usecase.PushValue(ctx, ref, req.GetKey(), raw); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *TaskValue) PullTaskValue(ctx context.Context, req *pb.TaskValuePullReq) (*pb.TaskValuePullRep, error) {
	v, err := h.usecase.PullValue(ctx, req.GetRunId(), req.GetTask(), req.GetKey())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.TaskValuePullRep{Value: dto.EncodeTaskValueJSON(v.Value)}, nil
}

// ListTaskValues — админский API (как ReadTaskLog): токеном не защищается.
func (h *TaskValue) ListTaskValues(ctx context.Context, req *pb.TaskValueListReq) (*pb.TaskValueListRep, error) {
	items, err := h.usecase.ListValues(ctx, req.GetRunId())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.TaskValueListRep{Values: lo.Map(items, dto.EncodeTaskValueMain)}, nil
}
