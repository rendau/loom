package service

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/pool/model"
)

type RepoDbI interface {
	List(ctx context.Context) ([]*model.Main, error)
	Set(ctx context.Context, name string, slots int) error
	ListMissing(ctx context.Context, names []string) ([]string, error)
}
