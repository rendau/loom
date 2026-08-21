package dagsync

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/rendau/loom/server/internal/infra/metrics"
)

var (
	metricSyncUpdates prometheus.Counter
	metricSyncErrors  prometheus.Counter
)

func init() {
	metricSyncUpdates = metrics.Factory.NewCounter(prometheus.CounterOpts{
		Namespace: "loom",
		Subsystem: "dagsync",
		Name:      "updates_total",
		Help:      "Авто-перерегистрации дагов по новой версии образа.",
	})

	metricSyncErrors = metrics.Factory.NewCounter(prometheus.CounterOpts{
		Namespace: "loom",
		Subsystem: "dagsync",
		Name:      "errors_total",
		Help:      "Ошибки авто-обновления дагов (digest-чек или перерегистрация).",
	})
}
