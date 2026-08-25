package manifest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
)

// описание env-привязки необязательно: манифесты дагов на старом SDK
// разбираются как раньше, просто с пустым Description
func TestParseEnvDescription(t *testing.T) {
	raw := []byte(`{
		"sdk_version": "0.3.0",
		"name": "etl",
		"tasks": [{
			"name": "load",
			"variables": [
				{"env": "PG_DSN", "variable": "pg_dsn", "description": "DSN основной БД"},
				{"env": "BATCH", "variable": "batch_size"}
			],
			"secrets": [
				{"env": "S3_KEY", "secret": "s3_key", "description": "ключ доступа к бакету"}
			]
		}]
	}`)

	m, err := Parse(raw)
	require.NoError(t, err)
	require.Len(t, m.Tasks, 1)

	assert.Equal(t, []dagModel.VariableRef{
		{Env: "PG_DSN", Variable: "pg_dsn", Description: "DSN основной БД"},
		{Env: "BATCH", Variable: "batch_size"},
	}, m.Tasks[0].Variables)
	assert.Equal(t, []dagModel.SecretRef{
		{Env: "S3_KEY", Secret: "s3_key", Description: "ключ доступа к бакету"},
	}, m.Tasks[0].Secrets)
}
