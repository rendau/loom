package dto

import (
	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	domainModel "github.com/rendau/loom/server/internal/domain/project/model"
	projectregModel "github.com/rendau/loom/server/internal/domain/projectreg/model"
)

// domain → proto

func EncodeProjectMain(v *domainModel.Main, _ int) *pb.ProjectMain {
	if v == nil {
		return nil
	}

	result := &pb.ProjectMain{
		Name:           v.Name,
		Image:          v.Image,
		ImageDigest:    v.ImageDigest,
		ImageSizeBytes: v.ImageSizeBytes,
		AutoUpdate:     v.AutoUpdate,
		DagCount:       int32(v.DagCount),
		Templates:      lo.Map(v.Templates, EncodeProjectTemplateMain),
		CreatedAt:      timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	return result
}

func EncodeProjectTemplateMain(v domainModel.Template, _ int) *pb.ProjectTemplateMain {
	result := &pb.ProjectTemplateMain{
		Name:          v.Name,
		SdkVersion:    v.SdkVersion,
		Orphaned:      v.Orphaned,
		MaxActiveRuns: int32(v.MaxActiveRuns),
		Tasks:         lo.Map(v.Tasks, EncodeDagTaskMain),
		DagCount:      int32(v.DagCount),
		CreatedAt:     timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	return result
}

func EncodeProjectRegistrationMain(v *projectregModel.Main, _ int) *pb.ProjectRegistrationMain {
	if v == nil {
		return nil
	}

	result := &pb.ProjectRegistrationMain{
		Id:          v.Id,
		ProjectName: v.ProjectName,
		Image:       v.Image,
		Source:      v.Source,
		Status:      v.Status,
		Error:       v.Error,
		AutoUpdate:  v.AutoUpdate,
		CreateDags:  v.CreateDags,
		Result: lo.Map(v.Result, func(d projectregModel.DagResult, _ int) *pb.ProjectRegistrationDag {
			return &pb.ProjectRegistrationDag{Name: d.Name, Error: d.Error, Created: d.Created}
		}),
		CreatedAt: timestamppb.New(v.CreatedAt),
	}
	if !v.StartedAt.IsZero() {
		result.StartedAt = timestamppb.New(v.StartedAt)
	}
	if !v.FinishedAt.IsZero() {
		result.FinishedAt = timestamppb.New(v.FinishedAt)
	}
	return result
}

// proto → domain

func DecodeProjectListReq(v *pb.ProjectListReq) *domainModel.ListReq {
	if v == nil {
		return &domainModel.ListReq{}
	}
	return &domainModel.ListReq{
		ListParams: DecodeListParams(v.ListParams),
		AutoUpdate: v.AutoUpdate,
	}
}

func DecodeProjectRegistrationListReq(v *pb.ProjectRegistrationListReq) *projectregModel.ListReq {
	if v == nil {
		return &projectregModel.ListReq{}
	}
	return &projectregModel.ListReq{
		ProjectName: v.ProjectName,
		OnlyActive:  v.GetActive(),
		Limit:       v.GetLimit(),
	}
}

func DecodeProjectRegisterReq(v *pb.ProjectRegisterReq) projectregModel.EnqueueSpec {
	return projectregModel.EnqueueSpec{
		ProjectName: v.GetName(),
		Image:       v.GetImage(),
		AutoUpdate:  v.AutoUpdate,
		// по умолчанию заводим даги по всем дагам образа
		CreateDags: v.CreateDags == nil || v.GetCreateDags(),
	}
}
