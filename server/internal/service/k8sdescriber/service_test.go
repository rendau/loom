package k8sdescriber

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	loom "github.com/rendau/loom/sdk"
)

// testEnv — обвязка вокруг fake clientset: захват созданного Job'а и его
// describe_id, подмена list подов (лейбл Job'а проставляется на лету — fake
// фильтрует результат list по селектору), хук onCreate для имитации push'а
// от Job'а.
type testEnv struct {
	cs  *fake.Clientset
	svc *Service

	job      *batchv1.Job
	id       string
	onCreate func(e *testEnv)
}

func newEnv(pods ...corev1.Pod) *testEnv {
	e := &testEnv{cs: fake.NewClientset()}
	e.svc = New(e.cs, "ns", "server:5052", 5*time.Second, "dag-registry")
	e.svc.digestWait = 200 * time.Millisecond

	e.cs.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		e.job = action.(k8stesting.CreateAction).GetObject().(*batchv1.Job)
		for _, env := range e.job.Spec.Template.Spec.Containers[0].Env {
			if env.Name == loom.EnvDescribeID {
				e.id = env.Value
			}
		}
		if e.onCreate != nil {
			e.onCreate(e)
		}
		return false, nil, nil
	})
	e.cs.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		items := make([]corev1.Pod, len(pods))
		for i, p := range pods {
			p.Labels = map[string]string{jobLabelKey: e.job.GetName()}
			items[i] = p
		}
		return true, &corev1.PodList{Items: items}, nil
	})

	return e
}

func describePod(phase corev1.PodPhase, imageID string, exitCode int32) corev1.Pod {
	cs := corev1.ContainerStatus{Name: containerName, ImageID: imageID}
	if phase == corev1.PodSucceeded || phase == corev1.PodFailed {
		cs.State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: exitCode}}
	}
	return corev1.Pod{Status: corev1.PodStatus{
		Phase:             phase,
		ContainerStatuses: []corev1.ContainerStatus{cs},
	}}
}

func TestInspectSuccess(t *testing.T) {
	e := newEnv(describePod(corev1.PodRunning, "docker-pullable://registry/demo@sha256:abc", 0))
	e.onCreate = func(e *testEnv) {
		require.True(t, e.svc.Deliver(e.id, []byte(`{"name":"demo"}`), ""))
	}

	digest, manifest, err := e.svc.Inspect(context.Background(), "registry/demo:latest")
	require.NoError(t, err)
	assert.Equal(t, "registry/demo@sha256:abc", digest)
	assert.Equal(t, `{"name":"demo"}`, string(manifest))

	// describe_id одноразовый — повторная доставка не принимается
	assert.False(t, e.svc.Deliver(e.id, []byte(`{}`), ""))

	// спека Job'а: одноразовый describe без ретраев k8s, с дедлайном,
	// лейблами и env-контрактом push'а
	require.NotNil(t, e.job)
	container := e.job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, []string{"describe"}, container.Args)
	assert.Equal(t, "registry/demo:latest", container.Image)
	assert.Contains(t, container.Env, corev1.EnvVar{Name: loom.EnvDescribeID, Value: e.id})
	assert.Contains(t, container.Env, corev1.EnvVar{Name: loom.EnvServerAddr, Value: "server:5052"})
	assert.Equal(t, int32(0), *e.job.Spec.BackoffLimit)
	assert.NotNil(t, e.job.Spec.ActiveDeadlineSeconds)
	assert.Equal(t, managedByLabelValue, e.job.Spec.Template.Labels[managedByLabelKey])
	assert.Equal(t, e.job.Name, e.job.Spec.Template.Labels[jobLabelKey])
	assert.Equal(t, []corev1.LocalObjectReference{{Name: "dag-registry"}},
		e.job.Spec.Template.Spec.ImagePullSecrets)

	// Job удалён сразу после чтения результата
	deleted := false
	for _, action := range e.cs.Actions() {
		if action.GetVerb() == "delete" && action.GetResource().Resource == "jobs" {
			deleted = true
		}
	}
	assert.True(t, deleted)
}

func TestInspectValidationError(t *testing.T) {
	// SDK push'ит ошибку валидации дага и выходит с кодом 2
	e := newEnv(describePod(corev1.PodFailed, "", 2))
	e.onCreate = func(e *testEnv) {
		require.True(t, e.svc.Deliver(e.id, nil, "invalid dag: cycle detected"))
	}

	_, _, err := e.svc.Inspect(context.Background(), "registry/demo:latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "describe failed: invalid dag: cycle detected")
}

func TestInspectDigestFallbackToImage(t *testing.T) {
	// imageID без digest — пиннинга нет, возвращаем исходный ref
	e := newEnv(describePod(corev1.PodRunning, "sha256localid", 0))
	e.onCreate = func(e *testEnv) {
		e.svc.Deliver(e.id, []byte(`{}`), "")
	}

	digest, _, err := e.svc.Inspect(context.Background(), "local/demo:dev")
	require.NoError(t, err)
	assert.Equal(t, "local/demo:dev", digest)
}

func TestInspectPullFailFast(t *testing.T) {
	pod := corev1.Pod{Status: corev1.PodStatus{
		Phase: corev1.PodPending,
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: containerName,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{
				Reason:  "ImagePullBackOff",
				Message: "Back-off pulling image",
			}},
		}},
	}}
	e := newEnv(pod)

	_, _, err := e.svc.Inspect(context.Background(), "registry/ghost:latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "image pull failed")
	assert.Contains(t, err.Error(), "ImagePullBackOff")
}

func TestInspectPodFailedWithoutPush(t *testing.T) {
	// упал до push'а (например, образ вовсе не loom-даг) — причина из
	// terminated-статуса и хвоста логов
	e := newEnv(describePod(corev1.PodFailed, "", 2))

	_, _, err := e.svc.Inspect(context.Background(), "registry/demo:latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "describe pod failed")
	assert.Contains(t, err.Error(), "exit code 2")
	// хвост логов пода (fake clientset отдаёт фиксированное тело)
	assert.Contains(t, err.Error(), "fake logs")
}

func TestInspectPodSucceededWithoutPush(t *testing.T) {
	// образ со старым SDK: напечатал манифест в stdout и вышел, push'а не
	// будет — ошибка после exitGrace
	e := newEnv(describePod(corev1.PodSucceeded, "registry/demo@sha256:abc", 0))
	e.svc.exitGrace = 100 * time.Millisecond

	_, _, err := e.svc.Inspect(context.Background(), "registry/demo:latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without pushing catalog")
}

func TestInspectTimeout(t *testing.T) {
	// подов нет вовсе (например, нет квоты) и push'а нет — общий таймаут
	svc := New(fake.NewClientset(), "ns", "server:5052", 50*time.Millisecond, "")

	_, _, err := svc.Inspect(context.Background(), "registry/demo:latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "describe timeout")
}

func TestDeliverUnknownId(t *testing.T) {
	svc := New(fake.NewClientset(), "ns", "server:5052", time.Second, "")

	assert.False(t, svc.Deliver("unknown", []byte(`{}`), ""))
}
