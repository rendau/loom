package handler

import (
	"context"

	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/rendau/loom/api/server_v1"
	artifactModel "github.com/rendau/loom/server/internal/domain/artifact/model"
	artifactUsc "github.com/rendau/loom/server/internal/usecase/artifact"
)

type Artifact struct {
	pb.UnsafeArtifactServiceServer

	usecase *artifactUsc.Usecase
}

func NewArtifact(uc *artifactUsc.Usecase) *Artifact {
	return &Artifact{usecase: uc}
}

func (h *Artifact) ListRunArtifact(ctx context.Context, req *pb.ArtifactListReq) (*pb.ArtifactListRep, error) {
	items, err := h.usecase.List(ctx, req.GetRunId())
	if err != nil {
		return nil, encodeErr(err)
	}

	return &pb.ArtifactListRep{
		Results: lo.Map(items, func(v artifactModel.Info, _ int) *pb.ArtifactMain {
			result := &pb.ArtifactMain{
				Task:    v.Task,
				Attempt: v.Attempt,
				Name:    v.Name,
				State:   v.State,
				Size:    v.Size,
			}
			if !v.Modified.IsZero() {
				result.ModifiedAt = timestamppb.New(v.Modified)
			}
			return result
		}),
	}, nil
}

func (h *Artifact) GetStorageStats(ctx context.Context, _ *emptypb.Empty) (*pb.StorageStatsRep, error) {
	stats, err := h.usecase.StorageStats(ctx)
	if err != nil {
		return nil, encodeErr(err)
	}

	encode := func(v artifactModel.StorageDir) *pb.StorageDirStats {
		return &pb.StorageDirStats{UsedBytes: v.UsedBytes, TotalBytes: v.TotalBytes, FreeBytes: v.FreeBytes}
	}
	return &pb.StorageStatsRep{Data: encode(stats.Data), Logs: encode(stats.Logs)}, nil
}
