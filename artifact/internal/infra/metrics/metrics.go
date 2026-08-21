// Package metrics — общий Prometheus-реестр процесса: домены заводят свои
// метрики через Factory, system http server отдаёт реестр на /metrics.
package metrics

import (
	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	Registry *prometheus.Registry
	Factory  promauto.Factory

	// Grpc — стандартные grpc_server_* метрики; интерцепторы вешает
	// NewGrpcServer.
	Grpc *grpcprom.ServerMetrics
)

func init() {
	Registry = prometheus.NewRegistry()
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	Grpc = grpcprom.NewServerMetrics(grpcprom.WithServerHandlingTimeHistogram())
	Registry.MustRegister(Grpc)

	Factory = promauto.With(Registry)
}
