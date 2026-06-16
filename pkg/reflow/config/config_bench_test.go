package config

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkLoadConfig(b *testing.B) {
	path := filepath.Join(b.TempDir(), "reflow.yaml")
	if err := os.WriteFile(path, []byte(benchmarkConfigYAML), 0o600); err != nil {
		b.Fatalf("write benchmark config: %v", err)
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Load(path); err != nil {
			b.Fatalf("Load returned error: %v", err)
		}
	}
}

const benchmarkConfigYAML = `
sources:
  - network: stream
    address: "-"
    type: bytes

processor:
  type: builtin
  workers: 2
  builtin:
    drop_message: true
    drop_payload: false
    disable_packet_mapping: false
    aggregation_helpers:
      mpls_labels: 3
      ip_layers: 2

aggregators:
  - stream: flow_data
    window:
      idle_flush_after_ms: 10000
    periodic:
      every_ms: 60000
    key_fields:
      - src_addr
      - dst_addr
      - proto
      - src_port
      - dst_port
    template_id: 256
    sum:
      - bytes
      - packets
    current:
      - input_if
      - output_if
      - end_time_unix

encoder:
  type: json
  workers: 1
  json:
    flavor: canonical

sink:
  type: stdout
`

func BenchmarkLoadFromFlags(b *testing.B) {
	benchmarks := []struct {
		name  string
		flags *FlagConfig
	}{
		{
			name: "stream_pcap_to_json",
			flags: &FlagConfig{
				Inputs: []string{"stream:fixtures/input.pcap:pcap"},
				Output: "json:stdout",
			},
		},
		{
			name: "stream_pcap_aggregate_to_ipfix",
			flags: &FlagConfig{
				Inputs:    []string{"stream:fixtures/input.pcap:pcap"},
				Output:    "ipfix:udp:127.0.0.1:4739",
				Aggregate: true,
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cfg, generated, err := LoadFromFlags(bm.flags)
				if err != nil {
					b.Fatalf("LoadFromFlags returned error: %v", err)
				}
				if !generated {
					b.Fatalf("expected generated config")
				}
				if len(cfg.Sources) == 0 {
					b.Fatalf("expected at least one source")
				}
			}
		})
	}
}
