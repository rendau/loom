package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/rendau/loom/api/server_v1"
	userModel "github.com/rendau/loom/server/internal/domain/user/model"
	userUsc "github.com/rendau/loom/server/internal/usecase/user"
)

// User — управление пользователями; доступ ограничен ролью admin в
// auth-интерцепторе.
type User struct {
	pb.UnsafeUserServiceServer

	usecase *userUsc.Usecase
}

func NewUser(uc *userUsc.Usecase) *User {
	return &User{usecase: uc}
}

func (h *User) ListUser(ctx context.Context, _ *emptypb.Empty) (*pb.UserListRep, error) {
	items, err := h.usecase.List(ctx)
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.UserListRep{Results: lo.Map(items, encodeUserMain)}, nil
}

func (h *User) CreateUser(ctx context.Context, req *pb.UserCreateReq) (*pb.UserMain, error) {
	result, err := h.usecase.Create(ctx, userModel.CreateSpec{
		Username: req.GetUsername(),
		Password: req.GetPassword(),
		Role:     req.GetRole(),
		DagNames: req.GetDagNames(),
	})
	if err != nil {
		return nil, encodeErr(err)
	}
	return encodeUserMain(result, 0), nil
}

func (h *User) UpdateUser(ctx context.Context, req *pb.UserUpdateReq) (*emptypb.Empty, error) {
	err := h.usecase.Update(ctx, req.GetId(), userModel.UpdateSpec{
		Password:    req.Password,
		Role:        req.Role,
		DagNames:    req.GetDagNames(),
		SetDagNames: req.GetSetDagNames(),
	})
	if err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *User) DeleteUser(ctx context.Context, req *pb.UserDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, req.GetId()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}
