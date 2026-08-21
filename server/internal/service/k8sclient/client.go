// Package k8sclient — построение kubernetes-клиента: in-cluster либо по
// kubeconfig. Общий для k8sexecutor и k8sdescriber — один clientset на
// приложение.
package k8sclient

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// New возвращает clientset: kubeconfig пуст — in-cluster конфигурация,
// иначе — по файлу kubeconfig.
func New(kubeconfig string) (kubernetes.Interface, error) {
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
	return clientset, nil
}
