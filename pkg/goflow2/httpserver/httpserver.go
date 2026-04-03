package httpserver

import (
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config configures the HTTP server.
type Config struct {
	Addr          string
	StoreHTTPPath string
}

// StoreSource returns flowstore data for HTTP rendering.
type StoreSource func() []byte

// HealthHandler returns a handler for the health endpoint.
func HealthHandler(isCollecting func() bool) http.HandlerFunc {
	return func(wr http.ResponseWriter, r *http.Request) {
		if !isCollecting() {
			wr.WriteHeader(http.StatusServiceUnavailable)
			if _, err := wr.Write([]byte("Not OK\n")); err != nil {
				slog.Error("error writing HTTP", slog.String("error", err.Error()))
			}
			return
		}
		wr.WriteHeader(http.StatusOK)
		if _, err := wr.Write([]byte("OK\n")); err != nil {
			slog.Error("error writing HTTP", slog.String("error", err.Error()))
		}
	}
}

// StoreHandler returns a handler for the store endpoint.
func StoreHandler(store StoreSource) http.HandlerFunc {
	return func(wr http.ResponseWriter, r *http.Request) {
		body := store()
		if body == nil {
			wr.WriteHeader(http.StatusNotFound)
			if _, err := wr.Write([]byte("Not Found\n")); err != nil {
				slog.Error("error writing HTTP", slog.String("error", err.Error()))
			}
			return
		}
		wr.Header().Add("Content-Type", "application/json")
		wr.WriteHeader(http.StatusOK)
		if _, err := wr.Write(body); err != nil {
			slog.Error("error writing HTTP", slog.String("error", err.Error()))
		}
	}
}

// New constructs a mux with metrics, health, and store endpoints.
func New(cfg Config, store StoreSource, isCollecting func() bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/__health", HealthHandler(isCollecting))
	if cfg.StoreHTTPPath != "" && store != nil {
		mux.HandleFunc(cfg.StoreHTTPPath, StoreHandler(store))
	}

	return mux
}
