package app

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/rendau/loom/server/internal/config"
	"github.com/rendau/loom/server/internal/infra/metrics"
)

// SystemHttpServerCreate builds the system HTTP server that exposes
// service endpoints: /healthcheck, /metrics.
func SystemHttpServerCreate() *http.Server {
	mux := http.NewServeMux()

	// healthcheck
	mux.HandleFunc("/healthcheck", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// prometheus
	mux.Handle("/metrics", promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{}))

	return &http.Server{
		Addr:              ":" + config.Conf.SystemHttpPort,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       time.Minute,
		MaxHeaderBytes:    300 * 1024,
	}
}
