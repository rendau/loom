package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	variableModel "github.com/rendau/loom/server/internal/domain/variable/model"
	variableUsc "github.com/rendau/loom/server/internal/usecase/variable"
)

type Variable struct {
	pb.UnsafeVariableServiceServer

	usecase *variableUsc.Usecase
}

func NewVariable(uc *variableUsc.Usecase) *Variable {
	return &Variable{usecase: uc}
}

func (h *Variable) ListVariable(ctx context.Context, req *pb.VariableListReq) (*pb.VariableListRep, error) {
	items, err := h.usecase.List(ctx, req.DagName)
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.VariableListRep{Results: lo.Map(items, encodeVariableMain)}, nil
}

func (h *Variable) SetVariable(ctx context.Context, req *pb.VariableSetReq) (*emptypb.Empty, error) {
	if err := h.usecase.Set(ctx, req.GetDagName(), req.GetName(), req.GetValue()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Variable) DeleteVariable(ctx context.Context, req *pb.VariableDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, req.GetDagName(), req.GetName()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func encodeVariableMain(v *variableModel.Main, _ int) *pb.VariableMain {
	result := &pb.VariableMain{
		Name:      v.Name,
		Value:     v.Value,
		DagName:   v.DagName,
		CreatedAt: timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	return result
}
