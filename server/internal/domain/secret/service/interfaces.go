package service

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/secret/model"
)

type RepoDbI interface {
	Set(ctx context.Context, name string, value []byte) error
	Delete(ctx context.Context, name string) (bool, error)
	ListMeta(ctx context.Context) ([]*model.Meta, error)
	GetValues(ctx context.Context, names []string) (map[string][]byte, error)
}
