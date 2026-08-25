package service

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/variable/model"
)

type RepoDbI interface {
	Set(ctx context.Context, dagName, name, value string) error
	Delete(ctx context.Context, dagName, name string) (bool, error)
	List(ctx context.Context, dagName *string) ([]*model.Main, error)
	GetValues(ctx context.Context, dagName string, names []string) (map[string]model.Resolved, error)
}
