package service

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/setting/model"
)

type RepoDbI interface {
	Set(ctx context.Context, dagName, name, value string) error
	Delete(ctx context.Context, dagName, name string) (bool, error)
	List(ctx context.Context, dagName *string) ([]*model.Main, error)
	// GetValues — значения по скоупам для резолва: глобальный ('') и
	// перечисленные даги, map[dagName]map[name]value.
	GetValues(ctx context.Context, dagNames []string) (map[string]map[string]string, error)
}
