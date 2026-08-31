package service

import (
	"context"
	"time"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/domain/user/model"
)

type RepoDbI interface {
	Create(ctx context.Context, m *model.Main, passwordHash string) error
	Update(ctx context.Context, id string, passwordHash, role *string) error
	Delete(ctx context.Context, id string) (bool, error)
	List(ctx context.Context) ([]*model.Main, error)
	Get(ctx context.Context, id string) (*model.Main, bool, error)
	GetByUsername(ctx context.Context, username string) (*model.Main, string, bool, error)
	CountUsers(ctx context.Context) (int64, error)
	LockUsers(ctx context.Context) error

	SetUserDags(ctx context.Context, userId string, dags []dagModel.Ref) error
	ListUserDags(ctx context.Context, userId string) ([]dagModel.Ref, error)
	HasUserDag(ctx context.Context, userId string, ref dagModel.Ref) (bool, error)
	HasUserDagInProject(ctx context.Context, userId, project string) (bool, error)
	SetUserProjects(ctx context.Context, userId string, projects []string) error
	ListUserProjects(ctx context.Context, userId string) ([]string, error)
	HasUserProject(ctx context.Context, userId, project string) (bool, error)

	CreateSession(ctx context.Context, tokenHash, userId string, expiresAt time.Time) error
	GetSessionUser(ctx context.Context, tokenHash string) (*model.Main, bool, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context) (int64, error)
}

type TxManagerI interface {
	TxFn(ctx context.Context, fn func(ctx context.Context) error) error
}
