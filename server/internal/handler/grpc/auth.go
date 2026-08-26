package handler

import (
	"context"

	"github.com/samber/lo"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	userModel "github.com/rendau/loom/server/internal/domain/user/model"
	"github.com/rendau/loom/server/internal/handler/grpc/dto"
	userUsc "github.com/rendau/loom/server/internal/usecase/user"
)

// TokenExtractorI — достаёт bearer-токен сессии из metadata (реализует
// app-слой, где живёт auth-интерцептор).
type TokenExtractorI func(ctx context.Context) string

type Auth struct {
	pb.UnsafeAuthServiceServer

	usecase *userUsc.Usecase
	token   TokenExtractorI
}

func NewAuth(uc *userUsc.Usecase, token TokenExtractorI) *Auth {
	return &Auth{usecase: uc, token: token}
}

func (h *Auth) GetAuthStatus(ctx context.Context, _ *emptypb.Empty) (*pb.AuthStatusRep, error) {
	exists, err := h.usecase.UsersExist(ctx)
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.AuthStatusRep{UsersExist: exists}, nil
}

func (h *Auth) CreateFirstAdmin(ctx context.Context, req *pb.AuthCreateFirstAdminReq) (*pb.AuthLoginRep, error) {
	token, user, expiresAt, err := h.usecase.CreateFirstAdmin(ctx, req.GetUsername(), req.GetPassword())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.AuthLoginRep{
		Token:     token,
		User:      encodeUserMain(user, 0),
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}

func (h *Auth) Login(ctx context.Context, req *pb.AuthLoginReq) (*pb.AuthLoginRep, error) {
	token, user, expiresAt, err := h.usecase.Login(ctx, req.GetUsername(), req.GetPassword())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.AuthLoginRep{
		Token:     token,
		User:      encodeUserMain(user, 0),
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}

func (h *Auth) Logout(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	if err := h.usecase.Logout(ctx, h.token(ctx)); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Auth) GetMe(ctx context.Context, _ *emptypb.Empty) (*pb.UserMain, error) {
	user, err := h.usecase.GetMe(ctx)
	if err != nil {
		return nil, encodeErr(err)
	}
	return encodeUserMain(user, 0), nil
}

func encodeUserMain(v *userModel.Main, _ int) *pb.UserMain {
	if v == nil {
		return nil
	}

	result := &pb.UserMain{
		Id:        v.Id,
		Username:  v.Username,
		Role:      v.Role,
		Dags:      lo.Map(v.Dags, dto.EncodeDagRef),
		Projects:  v.Projects,
		CreatedAt: timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	return result
}
