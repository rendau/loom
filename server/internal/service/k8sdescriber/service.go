// Package k8sdescriber — регистрация дагов без docker-демона (решение №29):
// манифест и digest образа получаются одноразовым k8s Job'ом в режиме
// `describe`, образ тянет kubelet. Job сам отправляет манифест на control
// plane (RPC PushDagManifest с одноразовым describe_id из env); логи пода
// используются только для диагностики падений.
package k8sdescriber

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	loom "github.com/rendau/loom/sdk"
)

const (
	// managedByLabelValue отличается от лейбла executor'а ("loom") — informer
	// executor'а describe-поды видеть не должен.
	managedByLabelKey   = "app.kubernetes.io/managed-by"
	managedByLabelValue = "loom-describe"

	// jobLabelKey — лейбл пода со значением имени Job'а: по нему ищется под
	// попытки (свой, а не автолейбл job-name — не зависим от версии k8s).
	jobLabelKey = "loom.io/describe-job"

	containerName = "describe"

	pollTick       = time.Second
	cleanupTimeout = 30 * time.Second
	// cleanupTTL — ttlSecondsAfterFinished: страховка самоочистки Job'а при
	// падении server'а; в норме Job удаляется сразу после чтения результата.
	cleanupTTL = 15 * time.Minute

	// exitGrace — сколько ждать push после того, как под завершился успешно:
	// SDK шлёт манифест до выхода, но вызов мог ещё не доехать. Под, так и не
	// пушнувший, — образ со старым SDK без поддержки describe_id.
	exitGrace = 5 * time.Second

	// digestWait — сколько ждать публикации imageID в статусе пода после
	// получения манифеста; дальше — warning и регистрация без пиннинга.
	digestWait = 15 * time.Second

	// errLogTail — сколько байт логов пода включать в ошибку упавшего
	// describe (диагностика; данные логами не передаются).
	errLogTail = 2048
)

// pullFailReasons — waiting-статусы контейнера, означающие, что образ не
// стянуть: ждать таймаута бессмысленно, фейлимся сразу.
var pullFailReasons = map[string]bool{
	"ErrImagePull":     true,
	"ImagePullBackOff": true,
	"InvalidImageName": true,
}

// pushResult — доставленный Job'ом ответ describe: манифест или ошибка
// валидации дага.
type pushResult struct {
	manifest []byte
	errMsg   string
}

type Service struct {
	clientset kubernetes.Interface
	namespace string
	// serverAddr — адрес control plane, каким его видит под Job'а
	// (TASK_SERVER_ADDR); уходит в env LOOM_SERVER_ADDR.
	serverAddr string
	timeout    time.Duration

	// поля вместо констант — тесты укорачивают ожидания
	exitGrace  time.Duration
	digestWait time.Duration

	mu      sync.Mutex
	waiters map[string]chan pushResult
}

func New(clientset kubernetes.Interface, namespace, serverAddr string, timeout time.Duration) *Service {
	return &Service{
		clientset:  clientset,
		namespace:  namespace,
		serverAddr: serverAddr,
		timeout:    timeout,
		exitGrace:  exitGrace,
		digestWait: digestWait,
		waiters:    map[string]chan pushResult{},
	}
}

// Inspect запускает Job образа в режиме `describe` и ждёт от него манифест
// (PushDagManifest по одноразовому describe_id). Возвращает пиннутый digest
// (imageID пода; без digest — warning и исходный ref) и JSON-манифест дага.
func (s *Service) Inspect(ctx context.Context, image string) (string, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	id, err := describeID()
	if err != nil {
		return "", nil, err
	}
	ch := s.addWaiter(id)
	defer s.removeWaiter(id)

	name := "loom-describe-" + id[:10]
	_, err = s.clientset.BatchV1().Jobs(s.namespace).Create(ctx, s.buildJob(name, image, id), metav1.CreateOptions{})
	if err != nil {
		return "", nil, fmt.Errorf("create describe job: %w", err)
	}
	defer s.cleanup(name)

	result, err := s.waitResult(ctx, name, ch)
	if err != nil {
		return "", nil, err
	}
	if result.errMsg != "" {
		return "", nil, fmt.Errorf("describe failed: %s", result.errMsg)
	}

	digest := s.awaitDigest(ctx, name)
	if digest == "" {
		slog.Warn("describe pod has no image digest, run will not be pinned", "image", image)
		digest = image
	}
	return digest, result.manifest, nil
}

// Deliver передаёт манифест от describe-Job'а ожидающей регистрации
// (вызывается из PushDagManifest). describe_id одноразовый: повторная или
// опоздавшая доставка — false.
func (s *Service) Deliver(id string, manifest []byte, errMsg string) bool {
	s.mu.Lock()
	ch, ok := s.waiters[id]
	if ok {
		delete(s.waiters, id)
	}
	s.mu.Unlock()

	if !ok {
		return false
	}
	ch <- pushResult{manifest: manifest, errMsg: errMsg} // буфер 1, доставка одна
	return true
}

func (s *Service) addWaiter(id string) chan pushResult {
	ch := make(chan pushResult, 1)
	s.mu.Lock()
	s.waiters[id] = ch
	s.mu.Unlock()
	return ch
}

func (s *Service) removeWaiter(id string) {
	s.mu.Lock()
	delete(s.waiters, id)
	s.mu.Unlock()
}

func (s *Service) buildJob(name, image, id string) *batchv1.Job {
	labels := map[string]string{
		managedByLabelKey: managedByLabelValue,
		jobLabelKey:       name,
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            new(int32(0)),
			TTLSecondsAfterFinished: new(int32(cleanupTTL / time.Second)),
			ActiveDeadlineSeconds:   new(int64(s.timeout / time.Second)),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{{
						Name:  containerName,
						Image: image,
						Args:  []string{"describe"},
						Env: []corev1.EnvVar{
							{Name: loom.EnvDescribeID, Value: id},
							{Name: loom.EnvServerAddr, Value: s.serverAddr},
						},
					}},
				},
			},
		},
	}
}

// waitResult ждёт push от Job'а, параллельно поллит под: ошибки pull и
// падение пода — fail-fast, успешный выход без push (старый SDK) — ошибка
// после exitGrace.
func (s *Service) waitResult(ctx context.Context, name string, ch chan pushResult) (pushResult, error) {
	ticker := time.NewTicker(pollTick)
	defer ticker.Stop()

	var exitedAt time.Time

	for {
		select {
		case result := <-ch:
			return result, nil
		case <-ctx.Done():
			return pushResult{}, fmt.Errorf("describe timeout (%s)", s.timeout)
		case <-ticker.C:
		}

		pod, err := s.findPod(ctx, name)
		if err != nil {
			return pushResult{}, err
		}
		if pod == nil {
			continue
		}

		switch pod.Status.Phase {
		case corev1.PodFailed:
			// push мог успеть до падения (ошибка валидации: SDK шлёт её и
			// выходит с кодом 2) — доставленный ответ точнее логов
			select {
			case result := <-ch:
				return result, nil
			default:
			}
			return pushResult{}, fmt.Errorf("describe pod failed: %s", s.failMessage(ctx, pod))

		case corev1.PodSucceeded:
			// под вышел успешно — push мог быть уже в полёте, даём догнать
			if exitedAt.IsZero() {
				exitedAt = time.Now()
			} else if time.Since(exitedAt) > s.exitGrace {
				return pushResult{}, fmt.Errorf(
					"describe pod finished without pushing manifest (image SDK does not support %s?)",
					loom.EnvDescribeID)
			}

		default:
			for _, cs := range pod.Status.ContainerStatuses {
				if w := cs.State.Waiting; w != nil && pullFailReasons[w.Reason] {
					return pushResult{}, fmt.Errorf("image pull failed: %s: %s", w.Reason, w.Message)
				}
			}
		}
	}
}

func (s *Service) findPod(ctx context.Context, name string) (*corev1.Pod, error) {
	pods, err := s.clientset.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: jobLabelKey + "=" + name,
	})
	if err != nil {
		return nil, fmt.Errorf("list describe pods: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, nil
	}
	return &pods.Items[0], nil
}

// awaitDigest достаёт пиннутый ref из imageID контейнера. Манифест приходит,
// пока под ещё Running, — kubelet мог не успеть опубликовать статус,
// поэтому короткий поллинг (не дольше digestWait).
func (s *Service) awaitDigest(ctx context.Context, name string) string {
	ctx, cancel := context.WithTimeout(ctx, s.digestWait)
	defer cancel()

	for {
		pod, err := s.findPod(ctx, name)
		if err == nil && pod != nil {
			if digest := imageDigest(pod); digest != "" {
				return digest
			}
		}

		select {
		case <-ctx.Done():
			return ""
		case <-time.After(pollTick):
		}
	}
}

// cleanup удаляет Job сразу после чтения результата (фоновым контекстом:
// вызывающий ctx к этому моменту может быть уже отменён); страховка при
// падении server'а — ttlSecondsAfterFinished.
func (s *Service) cleanup(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()

	propagation := metav1.DeletePropagationBackground
	err := s.clientset.BatchV1().Jobs(s.namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		slog.Warn("delete describe job failed", "job", name, "error", err)
	}
}

// failMessage — причина падения describe-пода: terminated-статус контейнера
// + хвост логов (диагностика: stderr дага виден только там).
func (s *Service) failMessage(ctx context.Context, pod *corev1.Pod) string {
	parts := make([]string, 0, 3)

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != containerName || cs.State.Terminated == nil {
			continue
		}
		t := cs.State.Terminated
		parts = append(parts, fmt.Sprintf("exit code %d", t.ExitCode))
		if t.Reason != "" && t.Reason != "Error" {
			parts = append(parts, t.Reason)
		}
		break
	}
	if len(parts) == 0 && pod.Status.Reason != "" {
		parts = append(parts, pod.Status.Reason)
	}

	logs, err := s.clientset.CoreV1().Pods(s.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Container:  containerName,
		LimitBytes: new(int64(errLogTail)),
	}).Do(ctx).Raw()
	if err != nil {
		slog.Warn("read describe pod logs failed", "pod", pod.Name, "error", err)
	} else if tail := strings.TrimSpace(string(logs)); tail != "" {
		parts = append(parts, tail)
	}

	if len(parts) == 0 {
		return "unknown reason"
	}
	return strings.Join(parts, ": ")
}

// imageDigest — пиннутый ref из imageID контейнера (containerd:
// `repo@sha256:...`, старый dockershim — с префиксом `docker-pullable://`).
func imageDigest(pod *corev1.Pod) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != containerName {
			continue
		}
		id := strings.TrimPrefix(cs.ImageID, "docker-pullable://")
		if strings.Contains(id, "@") {
			return id
		}
	}
	return ""
}

// describeID — одноразовый секрет describe-Job'а: по нему Job отдаёт
// манифест (PushDagManifest), первые 10 символов — суффикс имени Job'а.
func describeID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
