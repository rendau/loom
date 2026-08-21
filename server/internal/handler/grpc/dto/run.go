package dto

import (
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
	}
	if !v.FinishedAt.IsZero() {
		result.FinishedAt = timestamppb.New(v.FinishedAt)
	}
	return result
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
		Task:       v.Task,
		Attempt:    v.Attempt,
		Status:     v.Status,
		CreatedAt:  timestamppb.New(v.CreatedAt),
		ExitCode:   v.ExitCode,
		ExitReason: v.ExitReason,
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
