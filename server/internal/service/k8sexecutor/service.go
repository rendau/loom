// Package k8sexecutor — kubernetes-реализация executor-порта планировщика
// (решение №8): 1 pod (Job с backoffLimit=0) = 1 attempt, ретраями управляет
// планировщик loom, не k8s. Наблюдение — informer по подам с лейблом loom;
// идентификация попытки — в аннотациях (значения лейблов ограничены 63
// символами, run id может быть длиннее).
package k8sexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

const (
	managedByLabelKey   = "app.kubernetes.io/managed-by"
	managedByLabelValue = "loom"

	annRunId   = "loom.io/run-id"
	annTask    = "loom.io/task"
	annAttempt = "loom.io/attempt"

	containerName = "task"

	informerResync = 5 * time.Minute
	eventsBuffer   = 1024
)

type Service struct {
	clientset kubernetes.Interface
	namespace string
	jobTTL    time.Duration

	events  chan runModel.ExecEvent
	factory informers.SharedInformerFactory
	stopCh  chan struct{}
}

func New(namespace, kubeconfig string, jobTTL time.Duration) (*Service, error) {
	var (
		conf *rest.Config
		err  error
	)
	if kubeconfig != "" {
		conf, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		conf, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return nil, fmt.Errorf("kubernetes.NewForConfig: %w", err)
	}

	return &Service{
		clientset: clientset,
		namespace: namespace,
		jobTTL:    jobTTL,
		events:    make(chan runModel.ExecEvent, eventsBuffer),
		stopCh:    make(chan struct{}),
	}, nil
}

// Start поднимает informer по подам loom-попыток и начинает публиковать
// события их жизненного цикла в Events.
func (s *Service) Start() error {
	s.factory = informers.NewSharedInformerFactoryWithOptions(
		s.clientset,
		informerResync,
		informers.WithNamespace(s.namespace),
		informers.WithTweakListOptions(func(o *metav1.ListOptions) {
			o.LabelSelector = managedByLabelKey + "=" + managedByLabelValue
		}),
	)

	podInformer := s.factory.Core().V1().Pods().Informer()
	_, err := podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { s.handlePod(obj, false) },
		UpdateFunc: func(_, obj any) { s.handlePod(obj, false) },
		DeleteFunc: func(obj any) { s.handlePod(obj, true) },
	})
	if err != nil {
		return fmt.Errorf("pod informer handler: %w", err)
	}

	s.factory.Start(s.stopCh)
	if !cache.WaitForCacheSync(s.stopCh, podInformer.HasSynced) {
		return fmt.Errorf("pod informer cache sync failed")
	}

	return nil
}

func (s *Service) Stop() {
	close(s.stopCh)
	if s.factory != nil {
		s.factory.Shutdown()
	}
}

func (s *Service) Events() <-chan runModel.ExecEvent {
	return s.events
}

// Launch создаёт Job попытки. Идемпотентен: уже существующий Job этой
// попытки — не ошибка.
func (s *Service) Launch(ctx context.Context, spec runModel.LaunchSpec) error {
	job := s.buildJob(spec)

	_, err := s.clientset.BatchV1().Jobs(s.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// Kill удаляет Job попытки вместе с подом.
func (s *Service) Kill(ctx context.Context, ref runModel.AttemptRef) error {
	propagation := metav1.DeletePropagationForeground

	err := s.clientset.BatchV1().Jobs(s.namespace).Delete(ctx, jobName(ref), metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete job: %w", err)
	}
	return nil
}

func (s *Service) buildJob(spec runModel.LaunchSpec) *batchv1.Job {
	labels := map[string]string{managedByLabelKey: managedByLabelValue}
	annotations := map[string]string{
		annRunId:   spec.Ref.RunId,
		annTask:    spec.Ref.Task,
		annAttempt: strconv.Itoa(int(spec.Ref.Attempt)),
	}

	env := make([]corev1.EnvVar, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName(spec.Ref),
			Namespace:   s.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            new(int32(0)),
			TTLSecondsAfterFinished: new(int32(s.jobTTL / time.Second)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  containerName,
						Image: spec.Image,
						Args:  []string{"run"},
						Env:   env,
					}},
				},
			},
		},
	}
}

// handlePod транслирует состояние пода в события попытки. Дубли (resync
// informer'а) схлопывает идемпотентная финализация планировщика.
func (s *Service) handlePod(obj any, deleted bool) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		// удаление может прийти обёрнутым в DeletedFinalStateUnknown
		if tombstone, tOk := obj.(cache.DeletedFinalStateUnknown); tOk {
			pod, ok = tombstone.Obj.(*corev1.Pod)
		}
		if !ok {
			return
		}
	}

	ref, ok := podRef(pod)
	if !ok {
		return
	}

	switch pod.Status.Phase {
	case corev1.PodRunning:
		if !deleted {
			s.emit(runModel.ExecEvent{Ref: ref, Type: runModel.ExecEventStarted})
			return
		}
		// под удалён на бегу (нода умерла, eviction) — попытка провалена
		s.emit(finishedEvent(ref, pod, false, "pod_deleted"))

	case corev1.PodSucceeded:
		s.emit(finishedEvent(ref, pod, true, ""))

	case corev1.PodFailed:
		s.emit(finishedEvent(ref, pod, false, pod.Status.Reason))

	default: // Pending/Unknown
		if deleted {
			s.emit(finishedEvent(ref, pod, false, "pod_deleted"))
		}
	}
}

func (s *Service) emit(ev runModel.ExecEvent) {
	select {
	case s.events <- ev:
	default:
		// переполнение буфера: событие теряется, состояние догонит resync
		slog.Warn("k8s executor events buffer full, event dropped",
			"run_id", ev.Ref.RunId, "task", ev.Ref.Task, "attempt", ev.Ref.Attempt)
	}
}

func finishedEvent(ref runModel.AttemptRef, pod *corev1.Pod, success bool, fallbackReason string) runModel.ExecEvent {
	exit := runModel.ExitInfo{Success: success, Reason: fallbackReason}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != containerName || cs.State.Terminated == nil {
			continue
		}
		exit.ExitCode = new(cs.State.Terminated.ExitCode)
		if cs.State.Terminated.Reason != "" {
			exit.Reason = cs.State.Terminated.Reason
		}
		exit.Message = cs.State.Terminated.Message
		break
	}

	return runModel.ExecEvent{Ref: ref, Type: runModel.ExecEventFinished, Exit: &exit}
}

func podRef(pod *corev1.Pod) (runModel.AttemptRef, bool) {
	runId := pod.Annotations[annRunId]
	task := pod.Annotations[annTask]
	attempt, err := strconv.Atoi(pod.Annotations[annAttempt])
	if runId == "" || task == "" || err != nil {
		return runModel.AttemptRef{}, false
	}
	return runModel.AttemptRef{RunId: runId, Task: task, Attempt: int32(attempt)}, true
}

// jobName — детерминированное DNS-1123-имя Job'а попытки: усечённое имя
// таска для читаемости + хэш полного ref'а для уникальности.
func jobName(ref runModel.AttemptRef) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", ref.RunId, ref.Task, ref.Attempt)))

	task := strings.ToLower(ref.Task)
	task = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, task)
	task = strings.Trim(task, "-")
	if len(task) > 20 {
		task = task[:20]
	}
	if task == "" {
		task = "task"
	}

	return fmt.Sprintf("loom-%s-%d-%s", task, ref.Attempt, hex.EncodeToString(sum[:])[:10])
}
