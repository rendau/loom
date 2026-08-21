package scheduler

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/rendau/loom/server/internal/infra/metrics"
)

// Бакеты длительностей: проходы планировщика — миллисекунды-секунды,
// таски и раны — секунды-часы.
var (
	passDurationBuckets = []float64{0.005, 0.02, 0.1, 0.5, 2, 10}
	workDurationBuckets = []float64{1, 5, 30, 120, 600, 3600, 21600}
)

var (
	metricPassDuration  prometheus.Histogram
	metricTaskInstances *prometheus.GaugeVec
	metricPoolSlots     *prometheus.GaugeVec
	metricPoolBusy      *prometheus.GaugeVec
	metricCronLag       prometheus.Gauge

	metricRunFinished     *prometheus.CounterVec
	metricRunDuration     *prometheus.HistogramVec
	metricAttemptFinished *prometheus.CounterVec
	metricAttemptDuration *prometheus.HistogramVec

	metricLaunches     prometheus.Counter
	metricLaunchErrors prometheus.Counter
)

func init() {
	metricPassDuration = metrics.Factory.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loom",
		Subsystem: "scheduler",
		Name:      "pass_duration_seconds",
		Help:      "Длительность прохода планировщика.",
		Buckets:   passDurationBuckets,
	})

	metricTaskInstances = metrics.Factory.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loom",
		Subsystem: "scheduler",
		Name:      "task_instances",
		Help:      "Таски в нетерминальных статусах (глубина очереди).",
	}, []string{"status"})

	metricPoolSlots = metrics.Factory.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loom",
		Subsystem: "scheduler",
		Name:      "pool_slots",
		Help:      "Размер пула слотов.",
	}, []string{"pool"})

	metricPoolBusy = metrics.Factory.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loom",
		Subsystem: "scheduler",
		Name:      "pool_busy",
		Help:      "Занятые слоты пула (попытки в starting/running).",
	}, []string{"pool"})

	metricCronLag = metrics.Factory.NewGauge(prometheus.GaugeOpts{
		Namespace: "loom",
		Subsystem: "scheduler",
		Name:      "cron_lag_seconds",
		Help:      "Максимальное отставание наступивших cron-тиков от «сейчас».",
	})

	metricRunFinished = metrics.Factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loom",
		Name:      "run_finished_total",
		Help:      "Завершённые раны.",
	}, []string{"status"})

	metricRunDuration = metrics.Factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loom",
		Name:      "run_duration_seconds",
		Help:      "Длительность рана от триггера до завершения.",
		Buckets:   workDurationBuckets,
	}, []string{"status"})

	metricAttemptFinished = metrics.Factory.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loom",
		Name:      "attempt_finished_total",
		Help:      "Финализированные попытки.",
	}, []string{"success", "reason"})

	metricAttemptDuration = metrics.Factory.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loom",
		Name:      "attempt_duration_seconds",
		Help:      "Длительность попытки от старта до финализации.",
		Buckets:   workDurationBuckets,
	}, []string{"success"})

	metricLaunches = metrics.Factory.NewCounter(prometheus.CounterOpts{
		Namespace: "loom",
		Subsystem: "executor",
		Name:      "launches_total",
		Help:      "Успешно запущенные попытки.",
	})

	metricLaunchErrors = metrics.Factory.NewCounter(prometheus.CounterOpts{
		Namespace: "loom",
		Subsystem: "executor",
		Name:      "launch_errors_total",
		Help:      "Ошибки запуска попыток (launch_failed).",
	})
}
