package dto

import (
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	domainModel "github.com/rendau/loom/server/internal/domain/dagreg/model"
)

// domain → proto

func EncodeDagRegistrationMain(v *domainModel.Main, _ int) *pb.DagRegistrationMain {
	if v == nil {
		return nil
	}

	result := &pb.DagRegistrationMain{
		Id:         v.Id,
		Image:      v.Image,
		Source:     v.Source,
		Status:     v.Status,
		Error:      v.Error,
		DagName:    v.DagName,
		Schedule:   v.Schedule,
		Catchup:    v.Catchup,
		Paused:     v.Paused,
		AutoUpdate: v.AutoUpdate,
		Pool:       v.Pool,
		CreatedAt:  timestamppb.New(v.CreatedAt),
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

func DecodeDagRegistrationListReq(v *pb.DagRegistrationListReq) *domainModel.ListReq {
	if v == nil {
		return &domainModel.ListReq{}
	}
	return &domainModel.ListReq{
		DagName:    v.DagName,
		OnlyActive: v.GetActive(),
		Limit:      v.GetLimit(),
	}
}

func DecodeDagRegisterReq(v *pb.DagRegisterReq) domainModel.EnqueueSpec {
	return domainModel.EnqueueSpec{
		Image:      v.GetImage(),
		Schedule:   v.Schedule,
		Catchup:    v.Catchup,
		Paused:     v.Paused,
		AutoUpdate: v.AutoUpdate,
		Pool:       v.Pool,
	}
}
