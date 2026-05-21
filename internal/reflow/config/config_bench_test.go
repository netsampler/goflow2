package config

import (
	"path/filepath"
	"testing"
)

func BenchmarkLoadSampleConfig(b *testing.B) {
	path := filepath.Join("..", "..", "..", "cmd", "reflow", "reflow-pcap-aggregate-to-json.yaml")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Load(path); err != nil {
			b.Fatalf("Load returned error: %v", err)
		}
	}
}

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
