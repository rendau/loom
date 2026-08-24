package user

import (
	"context"
	"fmt"
	"time"

	"github.com/rendau/loom/server/internal/authctx"
	"github.com/rendau/loom/server/internal/domain/user/model"
	"github.com/rendau/loom/server/internal/errs"
)

// Usecase — вход в админку и управление пользователями. Роль вызывающего
// проверяет auth-интерцептор (UserService целиком admin-only); здесь —
// правила, которые интерцептору не видны (самоудаление, смена своей роли).
type Usecase struct {
	svc ServiceI
}

func New(svc ServiceI) *Usecase {
	return &Usecase{svc: svc}
}

func (u *Usecase) UsersExist(ctx context.Context) (bool, error) {
	exists, err := u.svc.UsersExist(ctx)
	if err != nil {
		return false, fmt.Errorf("svc.UsersExist: %w", err)
	}
	return exists, nil
}

// CreateFirstAdmin — первичная настройка: создать администратора и сразу
// залогинить его.
func (u *Usecase) CreateFirstAdmin(ctx context.Context, username, password string) (string, *model.Main, time.Time, error) {
	if _, err := u.svc.CreateFirstAdmin(ctx, username, password); err != nil {
		return "", nil, time.Time{}, fmt.Errorf("svc.CreateFirstAdmin: %w", err)
	}
	return u.Login(ctx, username, password)
}

func (u *Usecase) Login(ctx context.Context, username, password string) (string, *model.Main, time.Time, error) {
	if username == "" || password == "" {
		return "", nil, time.Time{}, errs.InvalidCredentials
	}
	token, user, expiresAt, err := u.svc.Login(ctx, username, password)
	if err != nil {
		return "", nil, time.Time{}, err
	}
	return token, user, expiresAt, nil
}

func (u *Usecase) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	if err := u.svc.Logout(ctx, token); err != nil {
		return fmt.Errorf("svc.Logout: %w", err)
	}
	return nil
}

// GetMe — текущий пользователь (с назначенными дагами).
func (u *Usecase) GetMe(ctx context.Context) (*model.Main, error) {
	info, ok := authctx.Info(ctx)
	if !ok {
		return nil, errs.NotAuthorized
	}
	result, err := u.svc.Get(ctx, info.UserId)
	if err != nil {
		return nil, fmt.Errorf("svc.Get: %w", err)
	}
	return result, nil
}

func (u *Usecase) List(ctx context.Context) ([]*model.Main, error) {
	items, err := u.svc.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("svc.List: %w", err)
	}
	return items, nil
}

func (u *Usecase) Create(ctx context.Context, spec model.CreateSpec) (*model.Main, error) {
	result, err := u.svc.Create(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("svc.Create: %w", err)
	}
	return result, nil
}

func (u *Usecase) Update(ctx context.Context, id string, spec model.UpdateSpec) error {
	if id == "" {
		return errs.IdRequired
	}
	// нельзя разжаловать самого себя — иначе можно остаться без админов
	if info, ok := authctx.Info(ctx); ok && info.UserId == id &&
		spec.Role != nil && *spec.Role != model.RoleAdmin {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "нельзя снять роль admin с самого себя"}
	}
	if err := u.svc.Update(ctx, id, spec); err != nil {
		return fmt.Errorf("svc.Update: %w", err)
	}
	return nil
}

func (u *Usecase) Delete(ctx context.Context, id string) error {
	if id == "" {
		return errs.IdRequired
	}
	if info, ok := authctx.Info(ctx); ok && info.UserId == id {
		return errs.ErrFull{Err: errs.InvalidRequest, Desc: "нельзя удалить самого себя"}
	}
	if err := u.svc.Delete(ctx, id); err != nil {
		return fmt.Errorf("svc.Delete: %w", err)
	}
	return nil
}
