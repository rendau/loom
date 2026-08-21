package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	poolModel "github.com/rendau/loom/server/internal/domain/pool/model"
	poolUsc "github.com/rendau/loom/server/internal/usecase/pool"
)

type Pool struct {
	pb.UnsafePoolServiceServer

	usecase *poolUsc.Usecase
}

func NewPool(uc *poolUsc.Usecase) *Pool {
	return &Pool{usecase: uc}
}

func (h *Pool) ListPool(ctx context.Context, _ *emptypb.Empty) (*pb.PoolListRep, error) {
	items, err := h.usecase.List(ctx)
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.PoolListRep{Results: lo.Map(items, encodePoolMain)}, nil
}

func (h *Pool) SetPool(ctx context.Context, req *pb.PoolSetReq) (*emptypb.Empty, error) {
	if err := h.usecase.Set(ctx, req.GetName(), int(req.GetSlots())); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func encodePoolMain(v *poolModel.Main, _ int) *pb.PoolMain {
	result := &pb.PoolMain{
		Name:      v.Name,
		Slots:     int32(v.Slots),
		CreatedAt: timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	return result
}
