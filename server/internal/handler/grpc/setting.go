package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	settingModel "github.com/rendau/loom/server/internal/domain/setting/model"
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
	items, err := h.usecase.List(ctx, req.DagName)
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.SettingListRep{Results: lo.Map(items, encodeSettingMain)}, nil
}

func (h *Setting) SetSetting(ctx context.Context, req *pb.SettingSetReq) (*emptypb.Empty, error) {
	if err := h.usecase.Set(ctx, req.GetDagName(), req.GetName(), req.GetValue()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Setting) DeleteSetting(ctx context.Context, req *pb.SettingDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, req.GetDagName(), req.GetName()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func encodeSettingMain(v *settingModel.Main, _ int) *pb.SettingMain {
	return &pb.SettingMain{
		Name:       v.Name,
		Value:      v.Value,
		DagName:    v.DagName,
		ModifiedAt: timestamppb.New(v.ModifiedAt),
	}
}
