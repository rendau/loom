package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/rendau/loom/server/internal/authctx"
	userModel "github.com/rendau/loom/server/internal/domain/user/model"
)

type fakeAuthenticator struct {
	role string
	err  error
}

func (f fakeAuthenticator) Authenticate(context.Context, string) (userModel.AuthInfo, error) {
	if f.err != nil {
		return userModel.AuthInfo{}, f.err
	}
	return userModel.AuthInfo{UserId: "usr-1", Username: "u", Role: f.role}, nil
}

func ctxWithAuth(values ...string) context.Context {
	md := metadata.MD{}
	if len(values) > 0 {
		md.Set("authorization", values...)
	}
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestAuthRequired(t *testing.T) {
	assert.True(t, authRequired("/server_v1.DagService/RegisterDag"))
	assert.True(t, authRequired("/server_v1.RunService/TriggerRun"))
	assert.True(t, authRequired("/server_v1.TaskLogService/ReadTaskLog"))
	assert.True(t, authRequired("/server_v1.TaskValueService/ListTaskValues"))
	assert.True(t, authRequired("/server_v1.AuthService/GetMe"))
	assert.True(t, authRequired("/server_v1.UserService/ListUser"))

	// task-facing ручки — открыты внутри кластера (describe_id у манифеста)
	assert.False(t, authRequired("/server_v1.DagService/PushDagManifest"))
	assert.False(t, authRequired("/server_v1.TaskValueService/PushTaskValue"))
	assert.False(t, authRequired("/server_v1.TaskValueService/PullTaskValue"))
	assert.False(t, authRequired("/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"))

	// вход и первичная настройка — до сессии
	assert.False(t, authRequired("/server_v1.AuthService/Login"))
	assert.False(t, authRequired("/server_v1.AuthService/GetAuthStatus"))
	assert.False(t, authRequired("/server_v1.AuthService/CreateFirstAdmin"))
}

func TestAdminRequired(t *testing.T) {
	assert.True(t, adminRequired("/server_v1.UserService/ListUser"))
	assert.True(t, adminRequired("/server_v1.UserService/CreateUser"))
	assert.True(t, adminRequired("/server_v1.DagService/RegisterDag"))
	assert.True(t, adminRequired("/server_v1.DagService/DeleteDag"))
	assert.True(t, adminRequired("/server_v1.PoolService/SetPool"))

	// права на конкретный даг проверяет usecase — интерцептор не мешает
	assert.False(t, adminRequired("/server_v1.DagService/SetDagSchedule"))
	assert.False(t, adminRequired("/server_v1.RunService/TriggerRun"))
	assert.False(t, adminRequired("/server_v1.SecretService/GetSecretValue"))
}

func TestAuthenticate(t *testing.T) {
	admin := fakeAuthenticator{role: userModel.RoleAdmin}

	t.Run("valid session puts caller into context", func(t *testing.T) {
		ctx, err := authenticate(ctxWithAuth("Bearer tok"), admin, "/server_v1.RunService/ListRun")
		require.NoError(t, err)

		info, ok := authctx.Info(ctx)
		require.True(t, ok)
		assert.Equal(t, "usr-1", info.UserId)
		assert.True(t, info.IsAdmin())
	})

	t.Run("scheme case does not matter", func(t *testing.T) {
		_, err := authenticate(ctxWithAuth("bearer tok"), admin, "/server_v1.RunService/ListRun")
		require.NoError(t, err)
	})

	for name, ctx := range map[string]context.Context{
		"no metadata":  context.Background(),
		"no header":    ctxWithAuth(),
		"no scheme":    ctxWithAuth("tok"),
		"empty bearer": ctxWithAuth("Bearer "),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := authenticate(ctx, admin, "/server_v1.RunService/ListRun")
			require.Error(t, err)
			assert.Equal(t, codes.Unauthenticated, status.Code(err))
		})
	}

	t.Run("unknown token", func(t *testing.T) {
		_, err := authenticate(ctxWithAuth("Bearer tok"),
			fakeAuthenticator{err: fmt.Errorf("no session")}, "/server_v1.RunService/ListRun")
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	t.Run("admin-only method denied to regular user", func(t *testing.T) {
		_, err := authenticate(ctxWithAuth("Bearer tok"),
			fakeAuthenticator{role: userModel.RoleUser}, "/server_v1.UserService/ListUser")
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("regular user passes dag-scoped method", func(t *testing.T) {
		_, err := authenticate(ctxWithAuth("Bearer tok"),
			fakeAuthenticator{role: userModel.RoleUser}, "/server_v1.DagService/SetDagSchedule")
		require.NoError(t, err)
	})
}
