package service

import (
	"context"

	commonModel "github.com/rendau/loom/server/internal/domain/common/model"
	"github.com/rendau/loom/server/internal/domain/secret/model"
)

type RepoDbI interface {
	Set(ctx context.Context, scope commonModel.Scope, name string, value []byte) error
	Delete(ctx context.Context, scope commonModel.Scope, name string) (bool, error)
	Move(ctx context.Context, from, to commonModel.Scope, name string) (bool, error)
	Exists(ctx context.Context, scope commonModel.Scope, name string) (bool, error)
	ListMeta(ctx context.Context, scope *commonModel.Scope) ([]*model.Meta, error)
	// GetValues — значения по именам для скоупа дага (даг перекрывает
	// проект, проект — глобальный); GetValue — значение точного скоупа.
	GetValues(ctx context.Context, scope commonModel.Scope, names []string) (map[string]model.Resolved, error)
	GetValue(ctx context.Context, scope commonModel.Scope, name string) ([]byte, bool, error)
}
