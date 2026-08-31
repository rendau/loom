package project

import (
	"context"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/project/model"
	projectregModel "github.com/rendau/loom/server/internal/domain/projectreg/model"
)

type ServiceI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, name string, errNE bool) (*model.Main, bool, error)
	Register(ctx context.Context, spec model.RegisterSpec,
		catalog *dagModel.Catalog) (*model.Main, []model.TemplateEdit, error)
	SetAutoUpdate(ctx context.Context, name string, autoUpdate bool) error
	Delete(ctx context.Context, name string) error
	ListTemplates(ctx context.Context, project string) ([]*model.Template, error)
	GetTemplate(ctx context.Context, project, name string, errNE bool) (*model.Template, bool, error)
}

// DagServiceI — даги-инстансы проекта: регистрация заводит их по новым
// шаблонам образа, удаление проекта уносит каскадом.
type DagServiceI interface {
	Create(ctx context.Context, ref dagModel.Ref, template string) (*dagModel.Main, error)
	Get(ctx context.Context, ref dagModel.Ref, errNE bool) (*dagModel.Main, bool, error)
	ListByProject(ctx context.Context, project string) ([]*dagModel.Main, error)
}

// ImageInspectorI — инспекция образа проекта при регистрации: пиннутый
// digest и JSON-каталог (`describe`). Реализации: docker-CLI (dockercli) и
// одноразовый k8s Job (k8sdescriber) — pull и запуск контейнера являются
// деталью реализации.
type ImageInspectorI interface {
	Inspect(ctx context.Context, image string) (digest string, catalog []byte, err error)
}

// ImageSizerI — размер образа по registry API (без скачивания). Отдельно
// от инспектора: тот работает через docker/k8s, а размер одинаково
// доступен обоим через registry. Best effort — ошибка не валит регистрацию.
type ImageSizerI interface {
	ResolveSize(ctx context.Context, image string) (int64, error)
}

// CatalogSinkI — приём каталога от describe-Job'а (PushDagCatalog):
// доставка ожидающей регистрации по одноразовому describe_id.
// false — id неизвестен (регистрация не ждёт: опоздал, повтор или подбор).
type CatalogSinkI interface {
	Deliver(id string, catalog []byte, errMsg string) bool
}

// AuthzI — права вызывающего: проект целиком (регистрация, настройки);
// sync доступен шире — и владельцу дага проекта.
type AuthzI interface {
	RequireProject(ctx context.Context, project string) error
	RequireProjectSync(ctx context.Context, project string) error
	RequireAdmin(ctx context.Context) error
}

// RegistrationsI — очередь асинхронных регистраций проектов.
type RegistrationsI interface {
	Enqueue(ctx context.Context, spec projectregModel.EnqueueSpec) (*projectregModel.Main, error)
	Get(ctx context.Context, id string) (*projectregModel.Main, error)
	List(ctx context.Context, req *projectregModel.ListReq) ([]*projectregModel.Main, error)
}
