package app

import (
	"net/http"
	"time"

	"github.com/rendau/loom/artifact/internal/config"
)

// SystemHttpServerCreate builds the system HTTP server that exposes
// service endpoints: /healthcheck.
func SystemHttpServerCreate() *http.Server {
	mux := http.NewServeMux()

	// healthcheck
	mux.HandleFunc("/healthcheck", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return &http.Server{
		Addr:              ":" + config.Conf.SystemHttpPort,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       time.Minute,
		MaxHeaderBytes:    300 * 1024,
	}
}
