package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"

	commonPb "github.com/rendau/loom/api/common"
	pb "github.com/rendau/loom/api/server_v1"
	"github.com/rendau/loom/server/internal/handler/grpc/dto"
	projectUsc "github.com/rendau/loom/server/internal/usecase/project"
)

type Project struct {
	pb.UnsafeProjectServiceServer

	usecase *projectUsc.Usecase
}

func NewProject(uc *projectUsc.Usecase) *Project {
	return &Project{usecase: uc}
}

func (h *Project) RegisterProject(ctx context.Context, req *pb.ProjectRegisterReq) (*pb.ProjectRegisterRep, error) {
	reg, err := h.usecase.Register(ctx, dto.DecodeProjectRegisterReq(req))
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.ProjectRegisterRep{RegistrationId: reg.Id}, nil
}

func (h *Project) ListProject(ctx context.Context, req *pb.ProjectListReq) (*pb.ProjectListRep, error) {
	if req.ListParams == nil {
		req.ListParams = &commonPb.ListParamsSt{}
	}

	items, tCount, err := h.usecase.List(ctx, dto.DecodeProjectListReq(req))
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.ProjectListRep{
		PaginationInfo: &commonPb.PaginationInfoSt{
			Page:       req.ListParams.Page,
			PageSize:   req.ListParams.PageSize,
			TotalCount: tCount,
		},
		Results: lo.Map(items, dto.EncodeProjectMain),
	}, nil
}

func (h *Project) GetProject(ctx context.Context, req *pb.ProjectGetReq) (*pb.ProjectMain, error) {
	item, err := h.usecase.Get(ctx, req.GetName())
	if err != nil {
		return nil, encodeErr(err)
	}
	return dto.EncodeProjectMain(item, 0), nil
}

func (h *Project) SyncProject(ctx context.Context, req *pb.ProjectSyncReq) (*pb.ProjectSyncRep, error) {
	reg, err := h.usecase.Sync(ctx, req.GetName())
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.ProjectSyncRep{RegistrationId: reg.Id}, nil
}

func (h *Project) SetProjectAutoUpdate(ctx context.Context, req *pb.ProjectSetAutoUpdateReq) (*emptypb.Empty, error) {
	if err := h.usecase.SetAutoUpdate(ctx, req.GetName(), req.GetAutoUpdate()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Project) DeleteProject(ctx context.Context, req *pb.ProjectDeleteReq) (*emptypb.Empty, error) {
	if err := h.usecase.Delete(ctx, req.GetName()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Project) ListProjectRegistration(ctx context.Context, req *pb.ProjectRegistrationListReq) (*pb.ProjectRegistrationListRep, error) {
	items, err := h.usecase.ListRegistrations(ctx, dto.DecodeProjectRegistrationListReq(req))
	if err != nil {
		return nil, encodeErr(err)
	}
	return &pb.ProjectRegistrationListRep{Results: lo.Map(items, dto.EncodeProjectRegistrationMain)}, nil
}

func (h *Project) GetProjectRegistration(ctx context.Context, req *pb.ProjectRegistrationGetReq) (*pb.ProjectRegistrationMain, error) {
	item, err := h.usecase.GetRegistration(ctx, req.GetId())
	if err != nil {
		return nil, encodeErr(err)
	}
	return dto.EncodeProjectRegistrationMain(item, 0), nil
}

// PushDagCatalog — внутренний вызов describe-Job'а; вместо токена его
// авторизует одноразовый непубличный describe_id.
func (h *Project) PushDagCatalog(ctx context.Context, req *pb.DagPushCatalogReq) (*emptypb.Empty, error) {
	if err := h.usecase.PushCatalog(ctx, req.GetDescribeId(), req.GetCatalog(), req.GetError()); err != nil {
		return nil, encodeErr(err)
	}
	return &emptypb.Empty{}, nil
}
