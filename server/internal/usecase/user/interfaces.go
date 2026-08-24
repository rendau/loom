package user

import (
	"context"
	"time"

	"github.com/rendau/loom/server/internal/domain/user/model"
)

type ServiceI interface {
	UsersExist(ctx context.Context) (bool, error)
	CreateFirstAdmin(ctx context.Context, username, password string) (*model.Main, error)
	Create(ctx context.Context, spec model.CreateSpec) (*model.Main, error)
	Update(ctx context.Context, id string, spec model.UpdateSpec) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*model.Main, error)
	Get(ctx context.Context, id string) (*model.Main, error)

	Login(ctx context.Context, username, password string) (string, *model.Main, time.Time, error)
	Logout(ctx context.Context, token string) error
}
