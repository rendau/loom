package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	settingModel "github.com/rendau/loom/server/internal/domain/setting/model"
	"github.com/rendau/loom/server/internal/handler/grpc/dto"
	settingUsc "github.com/rendau/loom/server/internal/usecase/setting"
)

type Setting struct {
	pb.UnsafeSettingServiceServer

	usecase *settingUsc.Usecase
}

func NewSetting(uc *settingUsc.Usecase) *Setting {
	return &Setting{usecase: uc}
}

func (h *Setting) ListSetting(ctx context.Context, req *pb.SettingListReq) (*pb.SettingListRep, error) {
	items, err := h.usecase.List(ctx, dto.DecodeScopeFilter(req.GetScope()))
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.SettingListRep{Results: lo.Map(items, encodeSettingMain)}, nil
}

func (h *Setting) SetSetting(ctx context.Context, req *pb.SettingSetReq) (*emptypb.Empty, error) {
	if err := h.usecase.Set(ctx, dto.DecodeScope(req.GetScope()), req.GetName(), req.GetValue()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Setting) DeleteSetting(ctx context.Context, req *pb.SettingDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, dto.DecodeScope(req.GetScope()), req.GetName()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func encodeSettingMain(v *settingModel.Main, _ int) *pb.SettingMain {
	return &pb.SettingMain{
		Name:       v.Name,
		Value:      v.Value,
		Scope:      dto.EncodeScope(v.Scope),
		ModifiedAt: timestamppb.New(v.ModifiedAt),
	}
}
