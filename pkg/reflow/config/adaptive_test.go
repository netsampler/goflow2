package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAdaptiveSamplingConfigAndRateRetention(t *testing.T) {
	var source SourceConfig
	if err := yaml.Unmarshal([]byte(`network: af_packet
capture:
  interface: eth2
  sampling:
    location: kernel
    every: 16
    adaptive:
      enabled: true
      min_every: 4
      max_every: 128
      interval_ms: 2000
      recovery_after_ms: 60000
`), &source); err != nil {
		t.Fatal(err)
	}
	if err := NormalizeCapture(&source); err != nil {
		t.Fatal(err)
	}
	a := source.Capture.Sampling.Adaptive
	if a.MinEvery != 4 || a.MaxEvery != 128 || a.IntervalMS != 2000 || a.RecoveryAfterMS != 60000 || a.IncreaseFactor != 4 || a.DecreaseFactor != 2 {
		t.Fatalf("adaptive config: %+v", a)
	}
	cfg := Config{Sources: []SourceConfig{source}, Aggregators: []AggregatorConfig{{Fields: []AggregatorField{{Role: "key", Name: "sampling_rate"}, {Role: "sum", Name: "bytes"}}}}, Encoder: EncoderConfig{Type: "ipfix", TemplatedFlow: TemplatedFlowConfig{Data: TemplatedFlowDataConfig{Select: []string{"bytes", "sampling_rate"}}}}}
	if err := cfg.ValidateAdaptivePipeline(); err != nil {
		t.Fatal(err)
	}
	// This otherwise-valid pipeline would merge counters selected at different
	// probabilities. Require a divisor key instead of silently corrupting totals.
	cfg.Aggregators[0].Fields[0].Role = "current"
	if err := cfg.ValidateAdaptivePipeline(); err == nil || !strings.Contains(err.Error(), "key:sampling_rate") {
		t.Fatalf("mixed-rate guard: %v", err)
	}
}

func TestLoadAdaptivePipelinePreservesRate(t *testing.T) {
	raw := []byte(`sources:
  - network: af_packet
    capture:
      interface: eth2
      sampling:
        location: kernel
        every: 16
        adaptive:
          enabled: true
aggregators:
  - fields: ["key:sampling_rate", "sum:bytes"]
    window:
      idle_flush_after_ms: 1000
encoder:
  type: ipfix
  templated_flow:
    data:
      fields: [bytes, sampling_rate]
`)
	cfg, err := LoadBytes("adaptive.yaml", raw)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Sources[0].Capture.Sampling.Adaptive.Enabled || cfg.Sources[0].Capture.Sampling.Adaptive.IntervalMS != 1000 {
		t.Fatalf("adaptive defaults missing: %+v", cfg.Sources[0].Capture.Sampling.Adaptive)
	}
	mixed := strings.ReplaceAll(string(raw), "key:sampling_rate", "current:sampling_rate")
	if _, err := LoadBytes("adaptive.yaml", []byte(mixed)); err == nil || !strings.Contains(err.Error(), "key:sampling_rate") {
		t.Fatalf("config load accepted mixed-rate aggregation: %v", err)
	}
	passthrough := strings.ReplaceAll(string(raw), `fields: ["key:sampling_rate", "sum:bytes"]`, `fields: ["current:bytes"]`)
	passthrough = strings.ReplaceAll(passthrough, "    window:\n      idle_flush_after_ms: 1000\n", "")
	if _, err := LoadBytes("adaptive.yaml", []byte(passthrough)); err == nil || !strings.Contains(err.Error(), "passthrough schema") {
		t.Fatalf("config load accepted IPFIX passthrough schema without sampling rate: %v", err)
	}
}
