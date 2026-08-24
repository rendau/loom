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
		Name:          v.Name,
		Image:         v.Image,
		ImageDigest:   v.ImageDigest,
		Schedule:      v.Schedule,
		Paused:        v.Paused,
		AutoUpdate:    v.AutoUpdate,
		SdkVersion:    v.SdkVersion,
		Catchup:       v.Catchup,
		MaxActiveRuns: int32(v.MaxActiveRuns),
		Tasks:         lo.Map(v.Tasks, EncodeDagTaskMain),
		CreatedAt:     timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	if !v.NextRunAt.IsZero() {
		result.NextRunAt = timestamppb.New(v.NextRunAt)
	}
	return result
}

func EncodeDagTaskMain(v domainModel.Task, _ int) *pb.DagTaskMain {
	result := &pb.DagTaskMain{
		Name:          v.Name,
		DependsOn:     lo.Map(v.DependsOn, EncodeDagTaskDepMain),
		Retries:       int32(v.Retries),
		RetryDelaySec: int32(v.RetryDelaySec),
		TimeoutSec:    int32(v.TimeoutSec),
		Pool:          v.Pool,
		Priority:      int32(v.Priority),
		Secrets: lo.Map(v.Secrets, func(s domainModel.SecretRef, _ int) *pb.DagTaskEnvSecret {
			return &pb.DagTaskEnvSecret{Env: s.Env, Secret: s.Secret}
		}),
		Variables: lo.Map(v.Variables, func(vr domainModel.VariableRef, _ int) *pb.DagTaskEnvVariable {
			return &pb.DagTaskEnvVariable{Env: vr.Env, Variable: vr.Variable}
		}),
	}
	if v.Resources != nil {
		result.Resources = &pb.DagTaskResources{
			CpuRequest:    v.Resources.CPURequest,
			CpuLimit:      v.Resources.CPULimit,
			MemoryRequest: v.Resources.MemoryRequest,
			MemoryLimit:   v.Resources.MemoryLimit,
		}
	}
	return result
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
