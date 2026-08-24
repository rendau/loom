package service

import (
	"context"
	"time"

	"github.com/rendau/loom/server/internal/domain/dagreg/model"
)

type RepoDbI interface {
	Create(ctx context.Context, m *model.Main) error
	ClaimPending(ctx context.Context, limit int64) ([]*model.Main, error)
	Finish(ctx context.Context, id, status, errMsg, dagName string) error
	Get(ctx context.Context, id string) (*model.Main, bool, error)
	List(ctx context.Context, req *model.ListReq) ([]*model.Main, error)
	HasActive(ctx context.Context, image string) (bool, error)
	FailStale(ctx context.Context, startedBefore time.Time) (int64, error)
	DeleteFinishedBefore(ctx context.Context, before time.Time) (int64, error)
}

// ProcessorI — собственно обработка регистрации: pull + describe →
// валидация → сохранение дага. Возвращает имя дага, когда оно стало
// известно (даже при ошибке после разбора манифеста).
type ProcessorI interface {
	Process(ctx context.Context, reg *model.Main) (dagName string, err error)
}
