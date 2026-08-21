package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthRequired(t *testing.T) {
	assert.True(t, authRequired("/server_v1.DagService/RegisterDag"))
	assert.True(t, authRequired("/server_v1.RunService/TriggerRun"))
	assert.True(t, authRequired("/server_v1.TaskLogService/ReadTaskLog"))
	assert.True(t, authRequired("/server_v1.TaskValueService/ListTaskValues"))

	// task-facing ручки — на своих механизмах (attempt-токены, describe_id)
	assert.False(t, authRequired("/server_v1.DagService/PushDagManifest"))
	assert.False(t, authRequired("/server_v1.TaskLogService/PushTaskLog"))
	assert.False(t, authRequired("/server_v1.TaskValueService/PushTaskValue"))
	assert.False(t, authRequired("/server_v1.TaskValueService/PullTaskValue"))
	assert.False(t, authRequired("/grpc.reflection.v1.ServerReflection/ServerReflectionInfo"))
}

func TestCheckAdminToken(t *testing.T) {
	ctxWith := func(values ...string) context.Context {
		md := metadata.MD{}
		if len(values) > 0 {
			md.Set("authorization", values...)
		}
		return metadata.NewIncomingContext(context.Background(), md)
	}

	require.NoError(t, checkAdminToken(ctxWith("Bearer secret1"), "secret1"))
	require.NoError(t, checkAdminToken(ctxWith("bearer secret1"), "secret1")) // регистр схемы не важен

	for name, ctx := range map[string]context.Context{
		"no metadata":  context.Background(),
		"no header":    ctxWith(),
		"wrong token":  ctxWith("Bearer wrong"),
		"no scheme":    ctxWith("secret1"),
		"empty bearer": ctxWith("Bearer "),
	} {
		err := checkAdminToken(ctx, "secret1")
		require.Error(t, err, name)
		assert.Equal(t, codes.Unauthenticated, status.Code(err), name)
	}
}
