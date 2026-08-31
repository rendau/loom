package test

import (
	"context"
	"testing"
	"time"

	"github.com/mechta-market/mobone/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendau/loom/server/internal/authctx"
	"github.com/rendau/loom/server/internal/authz"
	commonRepoPg "github.com/rendau/loom/server/internal/domain/common/repo/pg"
	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	userModel "github.com/rendau/loom/server/internal/domain/user/model"
	userDb "github.com/rendau/loom/server/internal/domain/user/repo/db"
	userService "github.com/rendau/loom/server/internal/domain/user/service"
	"github.com/rendau/loom/server/internal/errs"
	runUsc "github.com/rendau/loom/server/internal/usecase/run"
)

func newUserSvc(t *testing.T, e *env) *userService.Service {
	t.Helper()
	txm := mobone.NewTransactionManager(e.pool)
	return userService.New(userDb.New(commonRepoPg.NewBase(e.pool, txm)), txm)
}

// Первичная настройка и вход: первый админ создаётся только пока
// пользователей нет; логин выдаёт рабочий токен сессии, logout его гасит.
func TestUsersAndSessions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	svc := newUserSvc(t, e)

	exists, err := svc.UsersExist(ctx)
	require.NoError(t, err)
	assert.False(t, exists)

	admin, err := svc.CreateFirstAdmin(ctx, "root", "sup3r-secret")
	require.NoError(t, err)
	assert.Equal(t, userModel.RoleAdmin, admin.Role)

	exists, err = svc.UsersExist(ctx)
	require.NoError(t, err)
	assert.True(t, exists)

	// повторная первичная настройка запрещена
	_, err = svc.CreateFirstAdmin(ctx, "root2", "sup3r-secret")
	assert.ErrorIs(t, err, errs.InvalidRequest)

	// вход: неверный пароль и неизвестный логин неразличимы для клиента
	_, _, _, err = svc.Login(ctx, "root", "wrong-password")
	assert.ErrorIs(t, err, errs.InvalidCredentials)
	_, _, _, err = svc.Login(ctx, "ghost", "sup3r-secret")
	assert.ErrorIs(t, err, errs.InvalidCredentials)

	token, user, expiresAt, err := svc.Login(ctx, "root", "sup3r-secret")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	assert.Equal(t, admin.Id, user.Id)
	assert.True(t, expiresAt.After(admin.CreatedAt))

	// сырой токен в БД не хранится — только его хэш
	var stored int
	require.NoError(t, e.pool.QueryRow(ctx,
		`SELECT count(*) FROM session WHERE token_hash = $1`, token).Scan(&stored))
	assert.Zero(t, stored)

	info, err := svc.Authenticate(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, admin.Id, info.UserId)
	assert.True(t, info.IsAdmin())

	require.NoError(t, svc.Logout(ctx, token))
	_, err = svc.Authenticate(ctx, token)
	assert.ErrorIs(t, err, errs.NotAuthorized)

	// удаление пользователя гасит его сессии (FK on delete cascade)
	token2, _, _, err := svc.Login(ctx, "root", "sup3r-secret")
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, admin.Id))
	_, err = svc.Authenticate(ctx, token2)
	assert.ErrorIs(t, err, errs.NotAuthorized)
}

// Права на даг: admin — любой, обычный пользователь — только назначенные;
// глобальный скоуп (пустое имя) — только admin.
func TestUserDagPermissions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	svc := newUserSvc(t, e)

	dagName := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "owned-dag",
		"tasks": [{"name": "a"}]
	}`)
	otherDag := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "foreign-dag",
		"tasks": [{"name": "a"}]
	}`)

	admin, err := svc.CreateFirstAdmin(ctx, "root", "sup3r-secret")
	require.NoError(t, err)

	user, err := svc.Create(ctx, userModel.CreateSpec{
		Username: "analyst",
		Password: "analyst-pass",
		Role:     userModel.RoleUser,
		Dags:     []dagModel.Ref{dagName},
	})
	require.NoError(t, err)
	assert.Equal(t, []dagModel.Ref{dagName}, user.Dags)

	adminInfo := userModel.AuthInfo{UserId: admin.Id, Role: userModel.RoleAdmin}
	userInfo := userModel.AuthInfo{UserId: user.Id, Role: userModel.RoleUser}

	for _, dag := range []dagModel.Ref{dagName, otherDag, {}} {
		allowed, aErr := svc.CanManageDag(ctx, adminInfo, dag)
		require.NoError(t, aErr)
		assert.True(t, allowed, "admin может всё, даг %q", dag)
	}

	allowed, err := svc.CanManageDag(ctx, userInfo, dagName)
	require.NoError(t, err)
	assert.True(t, allowed, "назначенный даг доступен")

	allowed, err = svc.CanManageDag(ctx, userInfo, otherDag)
	require.NoError(t, err)
	assert.False(t, allowed, "чужой даг недоступен")

	allowed, err = svc.CanManageDag(ctx, userInfo, dagModel.Ref{})
	require.NoError(t, err)
	assert.False(t, allowed, "глобальный скоуп — только admin")

	// назначенный проект открывает все его даги, включая заведённые позже
	projectUser, err := svc.Create(ctx, userModel.CreateSpec{
		Username: "project-analyst",
		Password: "analyst-pass",
		Role:     userModel.RoleUser,
		Projects: []string{otherDag.Project},
	})
	require.NoError(t, err)
	projectInfo := userModel.AuthInfo{UserId: projectUser.Id, Role: userModel.RoleUser}

	allowed, err = svc.CanManageDag(ctx, projectInfo, otherDag)
	require.NoError(t, err)
	assert.True(t, allowed, "даг назначенного проекта доступен")

	allowed, err = svc.CanManageDag(ctx, projectInfo, dagName)
	require.NoError(t, err)
	assert.False(t, allowed, "даг чужого проекта недоступен")

	// sync проекта шире прочих операций: доступен и владельцу дага проекта
	allowed, err = svc.CanSyncProject(ctx, userInfo, dagName.Project)
	require.NoError(t, err)
	assert.True(t, allowed, "владелец дага может обновить проект из registry")

	allowed, err = svc.CanManageProject(ctx, userInfo, dagName.Project)
	require.NoError(t, err)
	assert.False(t, allowed, "но настройки проекта менять не может")

	allowed, err = svc.CanSyncProject(ctx, userInfo, otherDag.Project)
	require.NoError(t, err)
	assert.False(t, allowed, "чужой проект недоступен и для sync")

	// повышение до admin очищает назначения (ему доступны все даги)
	require.NoError(t, svc.Update(ctx, user.Id, userModel.UpdateSpec{Role: new(userModel.RoleAdmin)}))
	updated, err := svc.Get(ctx, user.Id)
	require.NoError(t, err)
	assert.Equal(t, userModel.RoleAdmin, updated.Role)
	assert.Empty(t, updated.Dags)

	// дубль логина отклоняется
	_, err = svc.Create(ctx, userModel.CreateSpec{
		Username: "analyst", Password: "analyst-pass", Role: userModel.RoleUser,
	})
	assert.ErrorIs(t, err, errs.UserExists)

	// короткий пароль отклоняется
	_, err = svc.Create(ctx, userModel.CreateSpec{
		Username: "shorty", Password: "123", Role: userModel.RoleUser,
	})
	assert.ErrorIs(t, err, errs.InvalidRequest)
}

// Ресурсные проверки usecase-слоя: обычный пользователь не может
// триггерить чужой даг, а свой — может; вызовы без аутентификации
// (внутренние) не ограничиваются.
func TestRunUsecasePermissions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	svc := newUserSvc(t, e)

	owned := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "perm-owned",
		"tasks": [{"name": "a"}]
	}`)
	foreign := e.registerDag(t, `{
		"sdk_version": "0.1.0",
		"name": "perm-foreign",
		"tasks": [{"name": "a"}]
	}`)

	_, err := svc.CreateFirstAdmin(ctx, "root", "sup3r-secret")
	require.NoError(t, err)
	user, err := svc.Create(ctx, userModel.CreateSpec{
		Username: "analyst", Password: "analyst-pass",
		Role: userModel.RoleUser, Dags: []dagModel.Ref{owned},
	})
	require.NoError(t, err)

	userCtx := authctx.With(ctx, userModel.AuthInfo{UserId: user.Id, Role: userModel.RoleUser})
	usecase := runUsc.New(e.runSvc, e.dagSvc, e.scheduler, authz.New(svc))

	_, err = usecase.Trigger(userCtx, owned, nil)
	require.NoError(t, err, "свой даг триггерится")

	_, err = usecase.Trigger(userCtx, foreign, nil)
	assert.ErrorIs(t, err, errs.PermissionDenied, "чужой даг — отказ")

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	_, err = usecase.Backfill(userCtx, foreign, from, from.AddDate(0, 0, 1), nil)
	assert.ErrorIs(t, err, errs.PermissionDenied)

	// внутренние вызовы (без auth в контексте) не ограничиваются
	_, err = usecase.Trigger(ctx, foreign, nil)
	require.NoError(t, err)
}
