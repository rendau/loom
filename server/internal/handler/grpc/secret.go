package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	secretModel "github.com/rendau/loom/server/internal/domain/secret/model"
	secretUsc "github.com/rendau/loom/server/internal/usecase/secret"
)

type Secret struct {
	pb.UnsafeSecretServiceServer

	usecase *secretUsc.Usecase
}

func NewSecret(uc *secretUsc.Usecase) *Secret {
	return &Secret{usecase: uc}
}

func (h *Secret) ListSecret(ctx context.Context, _ *emptypb.Empty) (*pb.SecretListRep, error) {
	items, err := h.usecase.List(ctx)
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.SecretListRep{Results: lo.Map(items, encodeSecretMeta)}, nil
}

func (h *Secret) SetSecret(ctx context.Context, req *pb.SecretSetReq) (*emptypb.Empty, error) {
	if err := h.usecase.Set(ctx, req.GetName(), []byte(req.GetValue())); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Secret) DeleteSecret(ctx context.Context, req *pb.SecretDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, req.GetName()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func encodeSecretMeta(v *secretModel.Meta, _ int) *pb.SecretMetaMain {
	result := &pb.SecretMetaMain{
		Name:      v.Name,
		CreatedAt: timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	return result
}
