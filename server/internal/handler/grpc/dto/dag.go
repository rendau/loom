package dto

import (
	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	domainModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// domain → proto

func EncodeDagMain(v *domainModel.Main, _ int) *pb.DagMain {
	if v == nil {
		return nil
	}

	result := &pb.DagMain{
		Name:        v.Name,
		Image:       v.Image,
		ImageDigest: v.ImageDigest,
		Schedule:    v.Schedule,
		Paused:      v.Paused,
		SdkVersion:  v.SdkVersion,
		Tasks:       lo.Map(v.Tasks, EncodeDagTaskMain),
		CreatedAt:   timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	return result
}

func EncodeDagTaskMain(v domainModel.Task, _ int) *pb.DagTaskMain {
	return &pb.DagTaskMain{
		Name:      v.Name,
		DependsOn: lo.Map(v.DependsOn, EncodeDagTaskDepMain),
	}
}

func EncodeDagTaskDepMain(v domainModel.Dep, _ int) *pb.DagTaskDepMain {
	return &pb.DagTaskDepMain{Task: v.Task, Streamed: v.Streamed}
}

// proto → domain

func DecodeDagListReq(v *pb.DagListReq) *domainModel.ListReq {
	if v == nil {
		return &domainModel.ListReq{}
	}
	return &domainModel.ListReq{
		ListParams: DecodeListParams(v.ListParams),
		Paused:     v.Paused,
	}
}
