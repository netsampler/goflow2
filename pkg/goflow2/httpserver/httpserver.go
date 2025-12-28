package httpserver

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config configures the HTTP server.
type Config struct {
	Addr         string
	TemplatePath string
}

// TemplateSource returns templates for HTTP rendering.
type TemplateSource func() map[string]map[uint64]interface{}

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

// TemplatesHandler returns a handler for the templates endpoint.
func TemplatesHandler(templates TemplateSource) http.HandlerFunc {
	return func(wr http.ResponseWriter, r *http.Request) {
		values := templates()
		if values == nil {
			wr.WriteHeader(http.StatusNotFound)
			if _, err := wr.Write([]byte("Not Found\n")); err != nil {
				slog.Error("error writing HTTP", slog.String("error", err.Error()))
			}
			return
		}
		body, err := json.MarshalIndent(values, "", "  ")
		if err != nil {
			slog.Error("error writing JSON body for /templates", slog.String("error", err.Error()))
			wr.WriteHeader(http.StatusInternalServerError)
			if _, err := wr.Write([]byte("Internal Server Error\n")); err != nil {
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

// New constructs a mux with metrics, health, and templates endpoints.
func New(cfg Config, templates TemplateSource, isCollecting func() bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/__health", HealthHandler(isCollecting))
	if cfg.TemplatePath != "" && templates != nil {
		mux.HandleFunc(cfg.TemplatePath, TemplatesHandler(templates))
	}

	return mux
}
