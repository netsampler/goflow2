package main

import (
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	collector "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	common "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/proto"
)

func TestRunExportsMetricsOnEOF(t *testing.T) {
	exported := make(chan *collector.ExportMetricsServiceRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/metrics" {
			t.Errorf("metrics request = %s %s", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		req := new(collector.ExportMetricsServiceRequest)
		if err := proto.Unmarshal(body, req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case exported <- req:
		default:
			t.Error("unexpected extra metrics export")
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("OTEL_METRICS_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", server.URL+"/v1/metrics")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_COMPRESSION", "none")
	t.Setenv("OTEL_METRIC_EXPORT_INTERVAL", "3600000")
	t.Setenv("OTEL_SERVICE_NAME", "test-reflow")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "")

	dir := t.TempDir()
	input := filepath.Join(dir, "input.ndjson")
	output := filepath.Join(dir, "output.ndjson")
	if err := os.WriteFile(input, []byte("{\"packets\":1}\n{\"packets\":2}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.LoadFromFlags(&config.FlagConfig{
		Inputs: []string{"stream:" + input + ":json"},
		Output: "json:file:" + output,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg.LogLevel = "info"
	cfg.LogFormat = "text"
	cfg.Processor.Workers = 1
	cfg.Encoder.Workers = 1
	if err := runApplication(t.Context(), cfg, "", 1); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}

	var request *collector.ExportMetricsServiceRequest
	select {
	case request = <-exported:
	default:
		t.Fatal("metrics were not exported on EOF")
	}
	const payloads = 3 // Two input records and the source initialization event.
	wantCounters := map[string]int64{
		"reflow.source.events": 2,
		"reflow.decode.events": 2,
		"reflow.sink.payloads": payloads,
		"reflow.sink.bytes":    info.Size() - payloads, // Exclude newline framing.
	}
	wantTimings := map[string]int64{
		"decode/decode":   2,
		"process/process": 2,
		"encode/encode":   payloads,
		"encode/flush":    1,
		"sink/send":       payloads,
	}
	wantTimingSums := maps.Clone(wantTimings)
	wantDepth := map[string]int64{"decode": 0, "process": 0, "encode": 0}
	wantCapacity := map[string]int64{"decode": 64, "process": 64, "encode": 64}
	var serviceName string
	for _, resource := range request.GetResourceMetrics() {
		for _, attr := range resource.GetResource().GetAttributes() {
			if attr.GetKey() == "service.name" {
				serviceName = attr.GetValue().GetStringValue()
			}
		}
		for _, scope := range resource.GetScopeMetrics() {
			for _, m := range scope.GetMetrics() {
				if want, ok := wantCounters[m.GetName()]; ok {
					points := m.GetSum().GetDataPoints()
					if len(points) != 1 || points[0].GetAsInt() != want || !m.GetSum().GetIsMonotonic() {
						t.Errorf("%s = %v, want one monotonic sum of %d", m.GetName(), m.GetSum(), want)
					}
					delete(wantCounters, m.GetName())
				}
				switch m.GetName() {
				case "reflow.stage.duration.observations":
					for _, point := range m.GetSum().GetDataPoints() {
						key := metricAttribute(point.GetAttributes(), "stage") + "/" + metricAttribute(point.GetAttributes(), "operation")
						if want, ok := wantTimings[key]; !ok || point.GetAsInt() != want || !m.GetSum().GetIsMonotonic() {
							t.Errorf("%s observations = %d, want %d", key, point.GetAsInt(), want)
						}
						delete(wantTimings, key)
					}
				case "reflow.stage.duration.sum":
					for _, point := range m.GetSum().GetDataPoints() {
						key := metricAttribute(point.GetAttributes(), "stage") + "/" + metricAttribute(point.GetAttributes(), "operation")
						if point.GetAsDouble() < 0 || wantTimingSums[key] == 0 || m.GetUnit() != "s" || !m.GetSum().GetIsMonotonic() {
							t.Errorf("%s duration sum = %v %s", key, point.GetAsDouble(), m.GetUnit())
						}
						delete(wantTimingSums, key)
					}
				case "reflow.queue.depth", "reflow.queue.capacity":
					expected := wantDepth
					if m.GetName() == "reflow.queue.capacity" {
						expected = wantCapacity
					}
					for _, point := range m.GetGauge().GetDataPoints() {
						key := metricAttribute(point.GetAttributes(), "queue")
						if want, ok := expected[key]; !ok || point.GetAsInt() != want {
							t.Errorf("%s for %s = %d, want %d", m.GetName(), key, point.GetAsInt(), want)
						}
						delete(expected, key)
					}
				}
			}
		}
	}
	if serviceName != "test-reflow" || len(wantCounters)+len(wantTimings)+len(wantTimingSums)+len(wantDepth)+len(wantCapacity) != 0 {
		t.Fatalf("service.name=%q, missing counters=%v, timings=%v, timing sums=%v, queue depths=%v, queue capacities=%v",
			serviceName, wantCounters, wantTimings, wantTimingSums, wantDepth, wantCapacity)
	}
}

func metricAttribute(attrs []*common.KeyValue, key string) string {
	for _, attr := range attrs {
		if attr.GetKey() == key {
			return attr.GetValue().GetStringValue()
		}
	}
	return ""
}
