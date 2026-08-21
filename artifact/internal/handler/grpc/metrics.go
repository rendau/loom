package handler

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/rendau/loom/artifact/internal/infra/metrics"
)

var (
	metricActiveWriteStreams prometheus.Gauge
	metricActiveReadStreams  prometheus.Gauge
	metricReceivedBytes      prometheus.Counter
)

func init() {
	metricActiveWriteStreams = metrics.Factory.NewGauge(prometheus.GaugeOpts{
		Namespace: "loom",
		Subsystem: "artifact",
		Name:      "active_write_streams",
		Help:      "Открытые write-стримы артефактов.",
	})

	metricActiveReadStreams = metrics.Factory.NewGauge(prometheus.GaugeOpts{
		Namespace: "loom",
		Subsystem: "artifact",
		Name:      "active_read_streams",
		Help:      "Открытые read-стримы артефактов (включая follow).",
	})

	metricReceivedBytes = metrics.Factory.NewCounter(prometheus.CounterOpts{
		Namespace: "loom",
		Subsystem: "artifact",
		Name:      "received_bytes_total",
		Help:      "Байты данных артефактов, принятые write-стримами.",
	})
}
