package dag

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/dag/model"
	dagregModel "github.com/rendau/loom/server/internal/domain/dagreg/model"
)

type ServiceI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, name string, errNE bool) (*model.Main, bool, error)
	Register(ctx context.Context, image, imageDigest string, rawManifest []byte, m *model.Manifest, autoUpdate *bool) (*model.Main, error)
	SetSchedule(ctx context.Context, name, schedule string, catchup bool) error
	SetPaused(ctx context.Context, name string, paused bool) error
	SetAutoUpdate(ctx context.Context, name string, autoUpdate bool) error
	Delete(ctx context.Context, name string) error
}

// ImageInspectorI — инспекция образа дага при регистрации: пиннутый digest
// и JSON-манифест (`describe`). Реализации: docker-CLI (dockercli) и
// одноразовый k8s Job (k8sdescriber) — pull и запуск
// контейнера являются деталью реализации.
type ImageInspectorI interface {
	Inspect(ctx context.Context, image string) (digest string, manifest []byte, err error)
}

// ManifestSinkI — приём манифеста от describe-Job'а (PushDagManifest):
// доставка ожидающей регистрации по одноразовому describe_id.
// false — id неизвестен (регистрация не ждёт: опоздал, повтор или подбор).
type ManifestSinkI interface {
	Deliver(id string, manifest []byte, errMsg string) bool
}

// PoolCheckerI — проверка существования пулов из манифеста при регистрации:
// таск с неизвестным пулом навсегда завис бы в очереди.
type PoolCheckerI interface {
	CheckExist(ctx context.Context, names []string) error
}

// AuthzI — проверка прав вызывающего на даг (расписание, пауза).
type AuthzI interface {
	RequireDag(ctx context.Context, dagName string) error
}

// RegistrationsI — очередь асинхронных регистраций дагов (domain/dagreg).
type RegistrationsI interface {
	Enqueue(ctx context.Context, spec dagregModel.EnqueueSpec) (*dagregModel.Main, error)
	Get(ctx context.Context, id string) (*dagregModel.Main, error)
	List(ctx context.Context, req *dagregModel.ListReq) ([]*dagregModel.Main, error)
}
