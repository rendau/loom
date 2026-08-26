package service

import (
	"context"
	"time"

	"github.com/rendau/loom/server/internal/domain/projectreg/model"
	settingModel "github.com/rendau/loom/server/internal/domain/setting/model"
)

// SettingsI — TTL истории регистраций (глобальная настройка dag_reg_ttl).
type SettingsI interface {
	ResolveGlobal(ctx context.Context) (settingModel.Effective, error)
}

type RepoDbI interface {
	Create(ctx context.Context, m *model.Main) error
	ClaimPending(ctx context.Context, limit int64) ([]*model.Main, error)
	Finish(ctx context.Context, id, status, errMsg string, result []model.DagResult) error
	Get(ctx context.Context, id string) (*model.Main, bool, error)
	List(ctx context.Context, req *model.ListReq) ([]*model.Main, error)
	HasActive(ctx context.Context, projectName string) (bool, error)
	FailStale(ctx context.Context, startedBefore time.Time) (int64, error)
	DeleteFinishedBefore(ctx context.Context, before time.Time) (int64, error)
}

// ProcessorI — собственно обработка регистрации: pull + describe →
// валидация каталога → сохранение проекта, шаблонов и новых дагов.
// Возвращает итог по дагам образа (в том числе при ошибке — что успело
// разобраться).
type ProcessorI interface {
	Process(ctx context.Context, reg *model.Main) ([]model.DagResult, error)
}
