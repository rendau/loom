package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendau/loom/server/internal/domain/dag/model"
	"github.com/rendau/loom/server/internal/errs"
)

func validManifest() *model.Manifest {
	return &model.Manifest{
		SdkVersion: "0.1.0",
		Name:       "demo-etl",
		Schedule:   "@daily",
		Tasks: []model.Task{
			{Name: "extract"},
			{Name: "transform", DependsOn: []model.Dep{{Task: "extract", Streamed: true}}},
			{Name: "load", DependsOn: []model.Dep{{Task: "transform"}}},
		},
	}
}

func requireInvalidManifest(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	full, ok := errs.AsErrFull(err)
	require.True(t, ok)
	assert.Equal(t, errs.InvalidManifest, full.Err)
}

func TestValidateManifest_Valid(t *testing.T) {
	require.NoError(t, ValidateManifest(validManifest()))
}

func TestValidateManifest_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(m *model.Manifest)
	}{
		{"nil sdk version", func(m *model.Manifest) { m.SdkVersion = "" }},
		{"empty name", func(m *model.Manifest) { m.Name = "" }},
		{"bad name", func(m *model.Manifest) { m.Name = "bad/name" }},
		{"no tasks", func(m *model.Manifest) { m.Tasks = nil }},
		{"bad task name", func(m *model.Manifest) { m.Tasks[0].Name = "-lead-dash" }},
		{"duplicate task", func(m *model.Manifest) { m.Tasks[1].Name = "extract" }},
		{"self dependency", func(m *model.Manifest) {
			m.Tasks[0].DependsOn = []model.Dep{{Task: "extract"}}
		}},
		{"unknown dependency", func(m *model.Manifest) {
			m.Tasks[1].DependsOn = []model.Dep{{Task: "ghost"}}
		}},
		{"duplicate dependency", func(m *model.Manifest) {
			m.Tasks[2].DependsOn = []model.Dep{{Task: "transform"}, {Task: "transform", Streamed: true}}
		}},
		{"cycle", func(m *model.Manifest) {
			m.Tasks[0].DependsOn = []model.Dep{{Task: "load"}}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := validManifest()
			tt.mutate(m)
			requireInvalidManifest(t, ValidateManifest(m))
		})
	}
}

func TestValidateManifest_NilManifest(t *testing.T) {
	requireInvalidManifest(t, ValidateManifest(nil))
}
