package service

import (
	"context"

	"github.com/rendau/loom/server/internal/domain/project/model"
)

type RepoDbI interface {
	List(ctx context.Context, pars *model.ListReq) ([]*model.Main, int64, error)
	Get(ctx context.Context, name string) (*model.Main, bool, error)
	UpdateOrCreate(ctx context.Context, name string, obj *model.Edit) error
	Update(ctx context.Context, name string, obj *model.Edit) error
	Delete(ctx context.Context, name string) error

	SetTemplates(ctx context.Context, project string, items []model.TemplateEdit) error
	ListTemplates(ctx context.Context, project string) ([]*model.Template, error)
	GetTemplate(ctx context.Context, project, name string) (*model.Template, bool, error)
	CountDags(ctx context.Context, projects []string) (map[string]int, error)
}
