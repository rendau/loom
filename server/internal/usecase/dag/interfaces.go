package dag

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/dag/model"
	projectModel "github.com/rendau/loom/server/internal/domain/project/model"
	statsModel "github.com/rendau/loom/server/internal/domain/stats/model"
)

type ServiceI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, ref model.Ref, errNE bool) (*model.Main, bool, error)
	Create(ctx context.Context, ref model.Ref, template string) (*model.Main, error)
	ListLastRuns(ctx context.Context, refs []model.Ref, perDag int) (map[model.Ref][]model.LastRun, error)
	ListTaskResources(ctx context.Context, ref model.Ref) ([]*model.TaskResourcesEntry, error)
	SetTaskResources(ctx context.Context, ref model.Ref, task string, res model.TaskResources) error
	DeleteTaskResources(ctx context.Context, ref model.Ref, task string) error
	SetSchedule(ctx context.Context, ref model.Ref, schedule string, catchup bool) error
	SetPaused(ctx context.Context, ref model.Ref, paused bool) error
	SetPool(ctx context.Context, ref model.Ref, pool string) error
	Delete(ctx context.Context, ref model.Ref) error
}

// ProjectsI — проект и его каталог: даг заводится только от шаблона
// существующего образа.
type ProjectsI interface {
	GetTemplate(ctx context.Context, project, name string, errNE bool) (*projectModel.Template, bool, error)
}

// PoolCheckerI — проверка существования пула, назначаемого дагу: даг с
// неизвестным пулом навсегда завис бы в очереди.
type PoolCheckerI interface {
	CheckExist(ctx context.Context, names []string) error
}

// StatsI — агрегаты по таскам дага (domain/stats): «жирные таски» админки.
type StatsI interface {
	DagStats(ctx context.Context, ref model.Ref, lastRuns int64) (int64, []statsModel.TaskStat, error)
}

// AuthzI — проверка прав вызывающего: даг (расписание, пауза, ресурсы) и
// проект (заведение нового дага).
type AuthzI interface {
	RequireDag(ctx context.Context, ref model.Ref) error
	RequireProject(ctx context.Context, project string) error
}
