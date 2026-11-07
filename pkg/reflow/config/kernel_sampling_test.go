package config

import (
	"strings"
	"testing"
)

func TestDriverSamplingBackendsRetainAdaptiveRate(t *testing.T) {
	for _, network := range []string{"af_packet", "xdp", "ebpf", "pktmon"} {
		t.Run(network, func(t *testing.T) {
			source := SourceConfig{Network: network, Capture: CaptureConfig{Interface: "eth0",
				Sampling: SamplingConfig{Location: "kernel", Every: 16, Adaptive: AdaptiveSamplingConfig{Enabled: true}}}}
			if network == "pktmon" {
				source.Capture.Sampling.Location = "callback"
			}
			if err := NormalizeCapture(&source); err != nil {
				t.Fatal(err)
			}
			cfg := Config{Sources: []SourceConfig{source}, Encoder: EncoderConfig{Type: "json"},
				Aggregators: []AggregatorConfig{{Match: map[string]string{"source.network": network},
					Fields: []AggregatorField{{Name: "sampling_rate", Role: "key"}, {Name: "bytes", Role: "sum"}}}}}
			if err := cfg.ValidateAdaptivePipeline(); err != nil {
				t.Fatal(err)
			}
			cfg.Aggregators[0].Fields[0].Role = "current"
			if err := cfg.ValidateAdaptivePipeline(); err == nil || !strings.Contains(err.Error(), "key:sampling_rate") {
				t.Fatalf("adaptive %s aggregation lost its divisor: %v", network, err)
			}
		})
	}
}
