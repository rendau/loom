package scheduler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

func task(name string, deps ...dagModel.Dep) dagModel.Task {
	return dagModel.Task{Name: name, DependsOn: deps}
}

func dep(name string) dagModel.Dep         { return dagModel.Dep{Task: name} }
func streamedDep(name string) dagModel.Dep { return dagModel.Dep{Task: name, Streamed: true} }

func TestBuildPlan_PromoteRoots(t *testing.T) {
	tasks := []dagModel.Task{
		task("a"),
		task("b"),
		task("c", dep("a"), dep("b")),
	}
	p := buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusPending,
		"b": runModel.TaskStatusPending,
		"c": runModel.TaskStatusPending,
	})

	assert.ElementsMatch(t, []string{"a", "b"}, p.Promote)
	assert.Empty(t, p.UpstreamFailed)
	assert.False(t, p.RunDone)
}

func TestBuildPlan_NormalEdgeWaitsSuccess(t *testing.T) {
	tasks := []dagModel.Task{task("a"), task("b", dep("a"))}

	// отправитель ещё бежит — обычное ребро не готово
	p := buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusRunning,
		"b": runModel.TaskStatusPending,
	})
	assert.Empty(t, p.Promote)
	assert.False(t, p.RunDone)

	// успех отправителя — получатель готов
	p = buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusSuccess,
		"b": runModel.TaskStatusPending,
	})
	assert.Equal(t, []string{"b"}, p.Promote)
}

func TestBuildPlan_StreamedEdgeCoStart(t *testing.T) {
	tasks := []dagModel.Task{task("a"), task("b", streamedDep("a"))}

	for _, status := range []string{
		runModel.TaskStatusStarting,
		runModel.TaskStatusRunning,
		runModel.TaskStatusSuccess,
	} {
		p := buildPlan(tasks, map[string]string{
			"a": status,
			"b": runModel.TaskStatusPending,
		})
		assert.Equal(t, []string{"b"}, p.Promote, "sender status %s", status)
	}

	// queued отправителя — ещё рано
	p := buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusQueued,
		"b": runModel.TaskStatusPending,
	})
	assert.Empty(t, p.Promote)
}

func TestBuildPlan_UpstreamFailedCascade(t *testing.T) {
	tasks := []dagModel.Task{
		task("a"),
		task("b", dep("a")),
		task("c", dep("b")),
		task("d", streamedDep("b")),
	}
	p := buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusFailed,
		"b": runModel.TaskStatusPending,
		"c": runModel.TaskStatusPending,
		"d": runModel.TaskStatusPending,
	})

	assert.ElementsMatch(t, []string{"b", "c", "d"}, p.UpstreamFailed)
	assert.Empty(t, p.Promote)
	require.True(t, p.RunDone)
	assert.Equal(t, runModel.RunStatusFailed, p.RunStatus)
}

func TestBuildPlan_RunDoneSuccess(t *testing.T) {
	tasks := []dagModel.Task{task("a"), task("b", dep("a"))}
	p := buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusSuccess,
		"b": runModel.TaskStatusSuccess,
	})

	require.True(t, p.RunDone)
	assert.Equal(t, runModel.RunStatusSuccess, p.RunStatus)
}

func TestBuildPlan_RunNotDoneWhileActive(t *testing.T) {
	tasks := []dagModel.Task{task("a"), task("b", dep("a"))}

	// b ещё бежит — ран не завершён, даже если promote нечего
	p := buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusSuccess,
		"b": runModel.TaskStatusRunning,
	})
	assert.False(t, p.RunDone)
	assert.Empty(t, p.RunStatus)
}

func TestBuildPlan_FailedStreamedSenderFailsPendingReceiver(t *testing.T) {
	tasks := []dagModel.Task{task("a"), task("b", streamedDep("a"))}
	p := buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusFailed,
		"b": runModel.TaskStatusPending,
	})

	assert.Equal(t, []string{"b"}, p.UpstreamFailed)
	require.True(t, p.RunDone)
	assert.Equal(t, runModel.RunStatusFailed, p.RunStatus)
}

func TestBuildPlan_UpForRetryKeepsRunAlive(t *testing.T) {
	tasks := []dagModel.Task{
		task("a"),
		task("b", dep("a")),
		task("c", streamedDep("a")),
	}

	// ожидающий ретрая отправитель: потомки не каскадятся в upstream_failed,
	// не промоутятся (ни обычное ребро, ни стримовое), ран не завершается
	p := buildPlan(tasks, map[string]string{
		"a": runModel.TaskStatusUpForRetry,
		"b": runModel.TaskStatusPending,
		"c": runModel.TaskStatusPending,
	})

	assert.Empty(t, p.Promote)
	assert.Empty(t, p.UpstreamFailed)
	assert.False(t, p.RunDone)
}
