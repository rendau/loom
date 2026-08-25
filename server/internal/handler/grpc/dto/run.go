package dto

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	domainModel "github.com/rendau/loom/server/internal/domain/run/model"
)

// domain → proto

func EncodeRunMain(v *domainModel.Main, _ int) *pb.RunMain {
	if v == nil {
		return nil
	}

	result := &pb.RunMain{
		Id:          v.Id,
		DagName:     v.DagName,
		Image:       v.Image,
		ImageDigest: v.ImageDigest,
		Trigger:     v.Trigger,
		Status:      v.Status,
		CreatedAt:   timestamppb.New(v.CreatedAt),
		LogicalDate: timestamppb.New(v.LogicalDate),
	}
	if !v.FinishedAt.IsZero() {
		result.FinishedAt = timestamppb.New(v.FinishedAt)
	}
	if len(v.Params) > 0 {
		// params в БД — валидный JSON-объект (приходит через Struct);
		// битое значение не валит выдачу рана
		var st structpb.Struct
		if err := st.UnmarshalJSON(v.Params); err == nil {
			result.Params = &st
		}
	}
	return result
}

// DecodeRunParams конвертирует Struct запроса в raw JSON для домена;
// nil — параметры не заданы.
func DecodeRunParams(v *structpb.Struct) ([]byte, error) {
	if v == nil || len(v.GetFields()) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(v.AsMap())
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	return raw, nil
}

func EncodeTaskInstanceMain(v *domainModel.TaskInstance, _ int) *pb.TaskInstanceMain {
	if v == nil {
		return nil
	}

	result := &pb.TaskInstanceMain{
		Task:    v.Task,
		Status:  v.Status,
		Attempt: v.Attempt,
	}
	if !v.QueuedAt.IsZero() {
		result.QueuedAt = timestamppb.New(v.QueuedAt)
	}
	if !v.StartedAt.IsZero() {
		result.StartedAt = timestamppb.New(v.StartedAt)
	}
	if !v.RetryAt.IsZero() {
		result.RetryAt = timestamppb.New(v.RetryAt)
	}
	if !v.FinishedAt.IsZero() {
		result.FinishedAt = timestamppb.New(v.FinishedAt)
	}
	return result
}

func EncodeAttemptMain(v *domainModel.Attempt, _ int) *pb.AttemptMain {
	if v == nil {
		return nil
	}

	result := &pb.AttemptMain{
		Task:            v.Task,
		Attempt:         v.Attempt,
		Status:          v.Status,
		CreatedAt:       timestamppb.New(v.CreatedAt),
		ExitCode:        v.ExitCode,
		ExitReason:      v.ExitReason,
		PeakMemoryBytes: v.PeakMemoryBytes,
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

func DecodeRunListReq(v *pb.RunListReq) *domainModel.ListReq {
	if v == nil {
		return &domainModel.ListReq{}
	}
	return &domainModel.ListReq{
		ListParams: DecodeListParams(v.ListParams),
		DagName:    v.DagName,
		Status:     v.Status,
	}
}

func EncodeRunEnvMain(v domainModel.RunEnv, _ int) *pb.RunEnvMain {
	return &pb.RunEnvMain{
		Env:        v.Env,
		Kind:       v.Kind,
		Name:       v.Name,
		Scope:      v.Scope,
		Value:      v.Value,
		ResolvedAt: timestamppb.New(v.ResolvedAt),
	}
}
