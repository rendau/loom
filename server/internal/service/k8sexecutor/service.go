// Package k8sexecutor — kubernetes-реализация executor-порта планировщика
//: 1 pod (Job с backoffLimit=0) = 1 attempt, ретраями управляет
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
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sResource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"

	dagModel "github.com/rendau/loom/server/internal/domain/dag/model"
	runModel "github.com/rendau/loom/server/internal/domain/run/model"
)

const (
	managedByLabelKey   = "app.kubernetes.io/managed-by"
	managedByLabelValue = "loom"

	annRunId   = "loom.io/run-id"
	annTask    = "loom.io/task"
	annAttempt = "loom.io/attempt"

	// attemptHashLabelKey — хэш ref'а попытки в лейбле Job'а: по нему Kill
	// находит Job, не собирая его имя (в имени есть даг и оно не выводится
	// из одного ref'а).
	attemptHashLabelKey = "loom.io/attempt-hash"

	containerName = "task"

	// jobNamePrefix — префикс имён Job'ов попыток (loom task): поды тасков
	// отличаются от подов самого loom и artifact.
	jobNamePrefix = "lt-"
	// jobNameMaxLen — потолок имени Job'а: оно уезжает в значение лейбла
	// подов batch.kubernetes.io/job-name, а значение лейбла — 63 символа.
	jobNameMaxLen = 63
	// refHashLen — длина хэша ref'а в имени Job'а и в значении лейбла.
	refHashLen = 10

	informerResync = 5 * time.Minute
	eventsBuffer   = 1024
)

type Service struct {
	clientset kubernetes.Interface
	namespace string
	jobTTL    time.Duration
	// pullSecret — imagePullSecrets подов попыток (приватные образы дагов);
	// пусто — без секрета.
	pullSecret string

	// metricsClient/metricsTick — семплинг потребления памяти попыток через
	// metrics.k8s.io (metrics-server); tick <= 0 — выключен.
	metricsClient metricsclient.Interface
	metricsTick   time.Duration
	podLister     listersv1.PodLister
	// peaks — зафиксированные пики памяти живых попыток: событие metrics
	// эмитится только при росте.
	peaksMu sync.Mutex
	peaks   map[runModel.AttemptRef]int64

	events  chan runModel.ExecEvent
	factory informers.SharedInformerFactory
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func New(clientset kubernetes.Interface, metricsClient metricsclient.Interface, namespace string,
	jobTTL time.Duration, pullSecret string, metricsTick time.Duration,
) *Service {
	return &Service{
		clientset:     clientset,
		namespace:     namespace,
		jobTTL:        jobTTL,
		pullSecret:    pullSecret,
		metricsClient: metricsClient,
		metricsTick:   metricsTick,
		peaks:         map[runModel.AttemptRef]int64{},
		events:        make(chan runModel.ExecEvent, eventsBuffer),
		stopCh:        make(chan struct{}),
	}
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

	pods := s.factory.Core().V1().Pods()
	s.podLister = pods.Lister()

	podInformer := pods.Informer()
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

	if s.metricsClient != nil && s.metricsTick > 0 {
		s.wg.Go(s.metricsLoop)
	} else {
		slog.Info("k8s executor memory sampling disabled (K8S_METRICS_TICK=0)")
	}

	return nil
}

func (s *Service) Stop() {
	close(s.stopCh)
	s.wg.Wait()
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
	job, err := s.buildJob(spec)
	if err != nil {
		return err
	}

	_, err = s.clientset.BatchV1().Jobs(s.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create job: %w", err)
	}
	return nil
}

// Kill удаляет Job попытки вместе с подом. Идемпотентен: отсутствие Job'а —
// не ошибка.
func (s *Service) Kill(ctx context.Context, ref runModel.AttemptRef) error {
	propagation := metav1.DeletePropagationForeground

	jobs := s.clientset.BatchV1().Jobs(s.namespace)

	list, err := jobs.List(ctx, metav1.ListOptions{LabelSelector: attemptSelector(ref)})
	if err != nil {
		return fmt.Errorf("list jobs: %w", err)
	}

	for _, job := range list.Items {
		err = jobs.Delete(ctx, job.Name, metav1.DeleteOptions{PropagationPolicy: &propagation})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete job: %w", err)
		}
	}
	return nil
}

// attemptSelector — label-селектор Job'а конкретной попытки.
func attemptSelector(ref runModel.AttemptRef) string {
	return managedByLabelKey + "=" + managedByLabelValue +
		"," + attemptHashLabelKey + "=" + refHash(ref)
}

// ListAlive возвращает попытки, чьи Job'ы существуют в кластере (включая
// завершённые до TTL-очистки) — источник правды для зомби-детекта
// планировщика.
func (s *Service) ListAlive(ctx context.Context) ([]runModel.AttemptRef, error) {
	list, err := s.clientset.BatchV1().Jobs(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: managedByLabelKey + "=" + managedByLabelValue,
	})
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}

	result := make([]runModel.AttemptRef, 0, len(list.Items))
	for _, job := range list.Items {
		if ref, ok := annotationsRef(job.Annotations); ok {
			result = append(result, ref)
		}
	}
	return result, nil
}

func (s *Service) buildJob(spec runModel.LaunchSpec) (*batchv1.Job, error) {
	labels := map[string]string{
		managedByLabelKey:   managedByLabelValue,
		attemptHashLabelKey: refHash(spec.Ref),
	}
	annotations := map[string]string{
		annRunId:   spec.Ref.RunId,
		annTask:    spec.Ref.Task,
		annAttempt: strconv.Itoa(int(spec.Ref.Attempt)),
	}

	env := make([]corev1.EnvVar, 0, len(spec.Env))
	for k, v := range spec.Env {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}

	resources, err := containerResources(spec.Resources)
	if err != nil {
		return nil, err
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobName(spec),
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
					RestartPolicy:    corev1.RestartPolicyNever,
					ImagePullSecrets: pullSecretRefs(s.pullSecret),
					Containers: []corev1.Container{{
						Name:      containerName,
						Image:     spec.Image,
						Args:      []string{"run"},
						Env:       env,
						Resources: resources,
					}},
				},
			},
		},
	}, nil
}

// containerResources — requests/limits контейнера из манифеста таска;
// quantities валидируются при регистрации дага, ошибка здесь — только у
// ранов, записанных до ужесточения валидации.
func containerResources(r *dagModel.TaskResources) (corev1.ResourceRequirements, error) {
	result := corev1.ResourceRequirements{}
	if r == nil {
		return result, nil
	}

	set := func(dst *corev1.ResourceList, name corev1.ResourceName, value string) error {
		if value == "" {
			return nil
		}
		q, err := k8sResource.ParseQuantity(value)
		if err != nil {
			return fmt.Errorf("parse resource %s=%q: %w", name, value, err)
		}
		if *dst == nil {
			*dst = corev1.ResourceList{}
		}
		(*dst)[name] = q
		return nil
	}

	for _, item := range []struct {
		dst   *corev1.ResourceList
		name  corev1.ResourceName
		value string
	}{
		{&result.Requests, corev1.ResourceCPU, r.CPURequest},
		{&result.Limits, corev1.ResourceCPU, r.CPULimit},
		{&result.Requests, corev1.ResourceMemory, r.MemoryRequest},
		{&result.Limits, corev1.ResourceMemory, r.MemoryLimit},
	} {
		if err := set(item.dst, item.name, item.value); err != nil {
			return result, err
		}
	}

	return result, nil
}

// ── семплинг потребления памяти (metrics.k8s.io) ────────

// metricsLoop периодически снимает PodMetrics loom-подов и эмитит
// metrics-событие при росте пика памяти попытки. Гранулярность
// metrics-server 15–60s — у короткоживущих тасков пик может не измериться.
// Отсутствующий/недоступный metrics-server не спамит логи: после ошибки
// семплер замолкает на 10 тиков и пробует снова.
func (s *Service) metricsLoop() {
	ticker := time.NewTicker(s.metricsTick)
	defer ticker.Stop()

	var quietUntil time.Time
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
		}

		if time.Now().Before(quietUntil) {
			continue
		}
		if err := s.sampleMetrics(); err != nil {
			quietUntil = time.Now().Add(10 * s.metricsTick)
			slog.Warn("k8s executor memory sampling failed (metrics-server unavailable?)",
				"error", err, "retry_after", 10*s.metricsTick)
		}
	}
}

func (s *Service) sampleMetrics() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	list, err := s.metricsClient.MetricsV1beta1().PodMetricses(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: managedByLabelKey + "=" + managedByLabelValue,
	})
	if err != nil {
		return fmt.Errorf("list pod metrics: %w", err)
	}

	for _, pm := range list.Items {
		pod, gErr := s.podLister.Pods(s.namespace).Get(pm.Name)
		if gErr != nil {
			continue // под уже исчез из кэша
		}
		ref, ok := podRef(pod)
		if !ok {
			continue
		}

		var usage int64
		for _, c := range pm.Containers {
			usage += c.Usage.Memory().Value()
		}
		s.notePeak(ref, usage)
	}

	// прюнинг пиков — по живым подам informer'а, а не по списку метрик:
	// metrics-server может временно не отдавать под
	alive := map[runModel.AttemptRef]bool{}
	if pods, lErr := s.podLister.Pods(s.namespace).List(labels.Everything()); lErr == nil {
		for _, pod := range pods {
			if ref, ok := podRef(pod); ok {
				alive[ref] = true
			}
		}
		s.prunePeaks(alive)
	}
	return nil
}

// notePeak поднимает пик попытки и эмитит metrics-событие только при росте.
func (s *Service) notePeak(ref runModel.AttemptRef, usage int64) {
	s.peaksMu.Lock()
	grew := usage > s.peaks[ref]
	if grew {
		s.peaks[ref] = usage
	}
	s.peaksMu.Unlock()

	if grew {
		s.emit(runModel.ExecEvent{Ref: ref, Type: runModel.ExecEventMetrics, PeakMemoryBytes: &usage})
	}
}

// prunePeaks выбрасывает пики исчезнувших попыток.
func (s *Service) prunePeaks(alive map[runModel.AttemptRef]bool) {
	s.peaksMu.Lock()
	defer s.peaksMu.Unlock()
	for ref := range s.peaks {
		if !alive[ref] {
			delete(s.peaks, ref)
		}
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
	return annotationsRef(pod.Annotations)
}

func annotationsRef(annotations map[string]string) (runModel.AttemptRef, bool) {
	runId := annotations[annRunId]
	task := annotations[annTask]
	attempt, err := strconv.Atoi(annotations[annAttempt])
	if runId == "" || task == "" || err != nil {
		return runModel.AttemptRef{}, false
	}
	return runModel.AttemptRef{RunId: runId, Task: task, Attempt: int32(attempt)}, true
}

// jobName — детерминированное DNS-1123-имя Job'а попытки:
// lt-<даг>-<таск>-<попытка>-<хэш>. Имена дага и таска нормализуются под
// DNS-1123 и усекаются, только если вдвоём не влезают в остаток бюджета
// (63 минус префикс, разделители, номер попытки и хэш) — длинному имени
// достаётся всё, что не занял короткий сосед. Уникальность имени даёт хэш
// ref'а, а не эти части.
func jobName(spec runModel.LaunchSpec) string {
	hash := refHash(spec.Ref)
	attempt := strconv.Itoa(int(spec.Ref.Attempt))
	dag := namePart(spec.DagName, "dag")
	task := namePart(spec.Ref.Task, "task")

	// три разделителя между четырьмя частями после префикса
	budget := jobNameMaxLen - len(jobNamePrefix) - 3 - len(attempt) - len(hash)
	dag, task = fitNameParts(dag, task, budget)

	return fmt.Sprintf("%s%s-%s-%s-%s", jobNamePrefix, dag, task, attempt, hash)
}

// fitNameParts укладывает пару имён в budget: помещаются оба — не трогаем;
// не помещаются — короткий берёт свою длину целиком, остаток бюджета уходит
// длинному, и только если длинные оба — бюджет делится пополам.
func fitNameParts(dag, task string, budget int) (string, string) {
	if len(dag)+len(task) <= budget {
		return dag, task
	}

	half := budget / 2
	switch {
	case len(dag) <= half:
		return dag, cut(task, budget-len(dag))
	case len(task) <= budget-half:
		return cut(dag, budget-len(task)), task
	default:
		return cut(dag, half), cut(task, budget-half)
	}
}

// cut — усечение части имени до n символов без хвостовых дефисов (первый
// символ у namePart всегда alnum, так что пустой результат невозможен).
func cut(v string, n int) string {
	if len(v) <= n {
		return v
	}
	return strings.TrimRight(v[:n], "-")
}

// namePart — часть имени объекта k8s из имени дага/таска: нижний регистр,
// только [a-z0-9-], без ведущих и хвостовых дефисов; пусто — fallback.
func namePart(v, fallback string) string {
	v = strings.Trim(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, strings.ToLower(v)), "-")

	if v == "" {
		return fallback
	}
	return v
}

// refHash — детерминированный короткий хэш попытки: уникальность имени Job'а
// и значение лейбла, по которому попытка находится в кластере.
func refHash(ref runModel.AttemptRef) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", ref.RunId, ref.Task, ref.Attempt)))
	return hex.EncodeToString(sum[:])[:refHashLen]
}

// pullSecretRefs — imagePullSecrets из имени секрета; пусто — без секрета.
func pullSecretRefs(name string) []corev1.LocalObjectReference {
	if name == "" {
		return nil
	}
	return []corev1.LocalObjectReference{{Name: name}}
}
