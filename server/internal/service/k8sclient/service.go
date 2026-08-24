// Package k8sclient — построение kubernetes-клиентов: in-cluster либо по
// kubeconfig. Общий для k8sexecutor и k8sdescriber — один clientset на
// приложение; metrics-клиент (metrics.k8s.io) — для семплинга потребления
// памяти попыток.
package k8sclient

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// New возвращает clientset и metrics-клиент: kubeconfig пуст — in-cluster
// конфигурация, иначе — по файлу kubeconfig. Создание metrics-клиента не
// требует наличия metrics-server — ошибка появится только при вызовах.
func New(kubeconfig string) (kubernetes.Interface, metricsclient.Interface, error) {
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
		return nil, nil, fmt.Errorf("k8s config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return nil, nil, fmt.Errorf("kubernetes.NewForConfig: %w", err)
	}

	metrics, err := metricsclient.NewForConfig(conf)
	if err != nil {
		return nil, nil, fmt.Errorf("metricsclient.NewForConfig: %w", err)
	}
	return clientset, metrics, nil
}
