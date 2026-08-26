package service

import (
	"context"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/domain/setting/model"
)

type RepoDbI interface {
	Set(ctx context.Context, scope commonModel.Scope, name, value string) error
	Delete(ctx context.Context, scope commonModel.Scope, name string) (bool, error)
	List(ctx context.Context, scope *commonModel.Scope) ([]*model.Main, error)
	GetValues(ctx context.Context, scopes []commonModel.Scope) (map[commonModel.Scope]map[string]string, error)
}
