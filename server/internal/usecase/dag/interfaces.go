package dag

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/dag/model"
)

type ServiceI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, name string, errNE bool) (*model.Main, bool, error)
	Register(ctx context.Context, image, imageDigest string, rawManifest []byte, m *model.Manifest) (*model.Main, error)
	SetPaused(ctx context.Context, name string, paused bool) error
	Delete(ctx context.Context, name string) error
}

// ImageInspectorI — container-CLI: pull образа, резолв digest и получение
// манифеста запуском контейнера в режиме `describe`.
type ImageInspectorI interface {
	Pull(ctx context.Context, image string) error
	ResolveDigest(ctx context.Context, image string) (string, error)
	Describe(ctx context.Context, image string) ([]byte, error)
}
