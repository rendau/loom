package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	secretModel "github.com/rendau/loom/server/internal/domain/secret/model"
	"github.com/rendau/loom/server/internal/handler/grpc/dto"
	secretUsc "github.com/rendau/loom/server/internal/usecase/secret"
)

type Secret struct {
	pb.UnsafeSecretServiceServer

	usecase *secretUsc.Usecase
}

func NewSecret(uc *secretUsc.Usecase) *Secret {
	return &Secret{usecase: uc}
}

func (h *Secret) ListSecret(ctx context.Context, req *pb.SecretListReq) (*pb.SecretListRep, error) {
	items, err := h.usecase.List(ctx, dto.DecodeScopeFilter(req.GetScope()))
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.SecretListRep{Results: lo.Map(items, encodeSecretMeta)}, nil
}

func (h *Secret) SetSecret(ctx context.Context, req *pb.SecretSetReq) (*emptypb.Empty, error) {
	if err := h.usecase.Set(ctx, dto.DecodeScope(req.GetScope()), req.GetName(), []byte(req.GetValue())); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Secret) DeleteSecret(ctx context.Context, req *pb.SecretDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, dto.DecodeScope(req.GetScope()), req.GetName()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Secret) MoveSecret(ctx context.Context, req *pb.SecretMoveReq) (*emptypb.Empty, error) {
	err := h.usecase.Move(ctx,
		dto.DecodeScope(req.GetScope()), dto.DecodeScope(req.GetToScope()), req.GetName())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Secret) GetSecretValue(ctx context.Context, req *pb.SecretGetValueReq) (*pb.SecretValueRep, error) {
	value, err := h.usecase.GetValue(ctx, dto.DecodeScope(req.GetScope()), req.GetName())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.SecretValueRep{Value: string(value)}, nil
}

func encodeSecretMeta(v *secretModel.Meta, _ int) *pb.SecretMetaMain {
	result := &pb.SecretMetaMain{
		Name:      v.Name,
		Scope:     dto.EncodeScope(v.Scope),
		CreatedAt: timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	return result
}
