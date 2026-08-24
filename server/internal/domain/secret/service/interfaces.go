package service

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/secret/model"
)

type RepoDbI interface {
	Set(ctx context.Context, dagName, name string, value []byte) error
	Delete(ctx context.Context, dagName, name string) (bool, error)
	ListMeta(ctx context.Context, dagName *string) ([]*model.Meta, error)
	// GetValues — значения по именам для дага (локальный скоуп перекрывает
	// глобальный); GetValue — значение точного скоупа.
	GetValues(ctx context.Context, dagName string, names []string) (map[string][]byte, error)
	GetValue(ctx context.Context, dagName, name string) ([]byte, bool, error)
}
