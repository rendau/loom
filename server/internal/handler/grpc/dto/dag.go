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
		Project:          v.Project,
		Name:             v.Name,
		Template:         v.Template,
		TemplateOrphaned: v.TemplateOrphaned,
		Image:            v.Image,
		ImageDigest:      v.ImageDigest,
		Schedule:         v.Schedule,
		Paused:           v.Paused,
		AutoUpdate:       v.AutoUpdate,
		Pool:             v.Pool,
		SdkVersion:       v.SdkVersion,
		Catchup:          v.Catchup,
		MaxActiveRuns:    int32(v.MaxActiveRuns),
		Tasks:            lo.Map(v.Tasks, EncodeDagTaskMain),
		CreatedAt:        timestamppb.New(v.CreatedAt),
	}
	if !v.ModifiedAt.IsZero() {
		result.ModifiedAt = timestamppb.New(v.ModifiedAt)
	}
	if !v.NextRunAt.IsZero() {
		result.NextRunAt = timestamppb.New(v.NextRunAt)
	}
	result.LastRuns = lo.Map(v.LastRuns, func(lr domainModel.LastRun, _ int) *pb.DagLastRun {
		return &pb.DagLastRun{RunId: lr.RunId, Status: lr.Status}
	})
	return result
}

func EncodeDagTaskMain(v domainModel.Task, _ int) *pb.DagTaskMain {
	result := &pb.DagTaskMain{
		Name:          v.Name,
		DependsOn:     lo.Map(v.DependsOn, EncodeDagTaskDepMain),
		Retries:       int32(v.Retries),
		RetryDelaySec: int32(v.RetryDelaySec),
		TimeoutSec:    int32(v.TimeoutSec),
		Priority:      int32(v.Priority),
		Secrets: lo.Map(v.Secrets, func(s domainModel.SecretRef, _ int) *pb.DagTaskEnvSecret {
			return &pb.DagTaskEnvSecret{Env: s.Env, Secret: s.Secret, Description: s.Description}
		}),
		Variables: lo.Map(v.Variables, func(vr domainModel.VariableRef, _ int) *pb.DagTaskEnvVariable {
			return &pb.DagTaskEnvVariable{Env: vr.Env, Variable: vr.Variable, Description: vr.Description}
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
		Project:    v.Project,
		Template:   v.Template,
	}
}

// DecodeDagRef собирает ссылку на даг из запроса (проект + имя).
func DecodeDagRef(project, name string) domainModel.Ref {
	return domainModel.NewRef(project, name)
}

// DecodeDagRefFilter — необязательный фильтр по дагу: пара задаётся
// целиком, иначе фильтра нет (например, счётчики по всем дагам).
func DecodeDagRefFilter(project, name *string) *domainModel.Ref {
	if project == nil || name == nil {
		return nil
	}
	return new(domainModel.NewRef(*project, *name))
}

// EncodeDagRef — ссылка на даг в proto (права пользователя, ответы).
func EncodeDagRef(v domainModel.Ref, _ int) *pb.DagRef {
	return &pb.DagRef{Project: v.Project, Name: v.Name}
}

// DecodeDagRefPb — ссылка на даг из proto.
func DecodeDagRefPb(v *pb.DagRef, _ int) domainModel.Ref {
	if v == nil {
		return domainModel.Ref{}
	}
	return domainModel.NewRef(v.GetProject(), v.GetName())
}
