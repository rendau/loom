package artifact

import (
	"context"
	"io"

	"github.com/rendau/loom/server/internal/domain/artifact/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

// ServiceI — доступ к данным артефактов на artifact-сервере (artifactcli).
type ServiceI interface {
	ListRunArtifacts(ctx context.Context, runId string) ([]model.Info, error)
	GetStorageStats(ctx context.Context) (model.StorageStats, error)
	ReadArtifactTo(ctx context.Context, ref model.Ref, offset, limit int64, w io.Writer) error
}

type RunServiceI interface {
	Get(ctx context.Context, id string, errNE bool) (*runModel.Main, bool, error)
}
