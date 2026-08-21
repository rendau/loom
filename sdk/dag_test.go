package loom

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func nopTask(context.Context, *Runtime) error {
	return nil
}

func TestValidate(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		d := New("ok_dag", Schedule("0 3 * * *"))
		a := d.Task("a", nopTask)
		b := d.Task("b", nopTask, After(a))
		d.Task("c", nopTask, AfterStreamed(b))

		require.NoError(t, d.Validate())
	})

	t.Run("invalid dag name", func(t *testing.T) {
		d := New("Bad Name")
		assert.ErrorContains(t, d.Validate(), "invalid dag name")
	})

	t.Run("invalid task name", func(t *testing.T) {
		d := New("dag")
		d.Task("Bad Task", nopTask)
		assert.ErrorContains(t, d.Validate(), "invalid task name")
	})

	t.Run("duplicate task", func(t *testing.T) {
		d := New("dag")
		d.Task("a", nopTask)
		d.Task("a", nopTask)
		assert.ErrorContains(t, d.Validate(), "duplicate task name")
	})

	t.Run("nil fn", func(t *testing.T) {
		d := New("dag")
		d.Task("a", nil)
		assert.ErrorContains(t, d.Validate(), "nil fn")
	})

	t.Run("foreign dag dependency", func(t *testing.T) {
		other := New("other")
		foreign := other.Task("foreign", nopTask)

		d := New("dag")
		d.Task("a", nopTask, After(foreign))
		assert.ErrorContains(t, d.Validate(), "belongs to another dag")
	})

	t.Run("nil dependency", func(t *testing.T) {
		d := New("dag")
		d.Task("a", nopTask, After(nil))
		assert.ErrorContains(t, d.Validate(), "nil dependency")
	})

	t.Run("negative retries", func(t *testing.T) {
		d := New("dag")
		d.Task("a", nopTask, Retries(-1))
		assert.ErrorContains(t, d.Validate(), "negative retries")
	})

	t.Run("negative retry delay", func(t *testing.T) {
		d := New("dag")
		d.Task("a", nopTask, RetryDelay(-time.Second))
		assert.ErrorContains(t, d.Validate(), "negative retry delay")
	})

	t.Run("negative timeout", func(t *testing.T) {
		d := New("dag")
		d.Task("a", nopTask, Timeout(-time.Second))
		assert.ErrorContains(t, d.Validate(), "negative timeout")
	})
}

func TestManifest(t *testing.T) {
	d := New("etl", Schedule("0 3 * * *"))
	a := d.Task("extract", nopTask,
		Retries(2), RetryDelay(45*time.Second), Timeout(10*time.Minute),
		Resources(ResourceSpec{CPURequest: "250m", MemoryLimit: "512Mi"}))
	b := d.Task("transform", nopTask, AfterStreamed(a))
	d.Task("load", nopTask, After(b))

	m := d.Manifest()

	assert.Equal(t, Version, m.SDKVersion)
	assert.Equal(t, "etl", m.Name)
	assert.Equal(t, "0 3 * * *", m.Schedule)
	require.Len(t, m.Tasks, 3)

	assert.Equal(t, TaskManifest{
		Name:          "extract",
		Retries:       2,
		RetryDelaySec: 45,
		TimeoutSec:    600,
		Resources:     &ResourcesManifest{CPURequest: "250m", MemoryLimit: "512Mi"},
	}, m.Tasks[0])
	assert.Equal(t, TaskManifest{
		Name:      "transform",
		DependsOn: []DepManifest{{Task: "extract", Streamed: true}},
	}, m.Tasks[1])
	assert.Equal(t, TaskManifest{
		Name:      "load",
		DependsOn: []DepManifest{{Task: "transform"}},
	}, m.Tasks[2])
}
