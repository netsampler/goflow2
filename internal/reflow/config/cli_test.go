package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadFromFlagsGeneratedDefaults(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config mode")
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("expected 2 default sources, got %d", len(cfg.Sources))
	}
	if cfg.Sources[0].Network != "udp" || cfg.Sources[0].Address != ":6343" || cfg.Sources[0].Type != "flow" {
		t.Fatalf("unexpected first default source: %#v", cfg.Sources[0])
	}
	if cfg.Sources[1].Network != "udp" || cfg.Sources[1].Address != ":2055" || cfg.Sources[1].Type != "flow" {
		t.Fatalf("unexpected second default source: %#v", cfg.Sources[1])
	}
	if cfg.Encoder.Type != "json" {
		t.Fatalf("expected default encoder json, got %q", cfg.Encoder.Type)
	}
	if cfg.Sink.Type != "stdout" {
		t.Fatalf("expected default sink stdout, got %#v", cfg.Sink)
	}
}

func TestParseInputSpecSupportedForms(t *testing.T) {
	tests := []struct {
		spec      string
		network   string
		address   string
		iface     string
		sourceTyp string
	}{
		{spec: "udp:192.168.0.1:2055:flow", network: "udp", address: "192.168.0.1:2055", sourceTyp: "flow"},
		{spec: "udp:[::1]:6343:flow", network: "udp", address: "[::1]:6343", sourceTyp: "flow"},
		{spec: "socket:/tmp/reflow.sock:json", network: "unixgram", address: "/tmp/reflow.sock", sourceTyp: "json"},
		{spec: "unixgram:/tmp/reflow.sock:bytes", network: "unixgram", address: "/tmp/reflow.sock", sourceTyp: "bytes"},
		{spec: "stream:-:pcap", network: "stream", address: "-", sourceTyp: "pcap"},
		{spec: "stream:/tmp/events.ndjson:json", network: "stream", address: "/tmp/events.ndjson", sourceTyp: "json"},
		{spec: "pcap_live:en0:bytes", network: "pcap_live", iface: "en0", sourceTyp: "bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			src, err := parseInputSpec(tt.spec)
			if err != nil {
				t.Fatalf("parseInputSpec returned error: %v", err)
			}
			if src.Network != tt.network || src.Address != tt.address || src.Interface != tt.iface || src.Type != tt.sourceTyp {
				t.Fatalf("unexpected source for %q: %#v", tt.spec, src)
			}
			if src.Type == "json" && src.JSON.Flavor != "reflow" {
				t.Fatalf("expected helper json input to use reflow flavor, got %#v", src.JSON)
			}
		})
	}
}

func TestParseOutputSpecSupportedForms(t *testing.T) {
	tests := []struct {
		spec    string
		enc     string
		sink    string
		path    string
		address string
	}{
		{spec: "json:stdout", enc: "json", sink: "stdout"},
		{spec: "json:file:/tmp/reflow.ndjson", enc: "json", sink: "file", path: "/tmp/reflow.ndjson"},
		{spec: "ipfix:udp:192.168.0.2:2055", enc: "ipfix", sink: "udp", address: "192.168.0.2:2055"},
		{spec: "netflowv9:udp:[::1]:2055", enc: "netflowv9", sink: "udp", address: "[::1]:2055"},
		{spec: "protobuf:socket:/tmp/reflow.out", enc: "protobuf", sink: "unixgram", address: "/tmp/reflow.out"},
		{spec: "pcap:file:/tmp/reflow.pcap", enc: "pcap", sink: "file", path: "/tmp/reflow.pcap"},
		{spec: "pcapng:stdout", enc: "pcapng", sink: "stdout"},
		{spec: "sflow:udp:127.0.0.1:6343", enc: "sflow", sink: "udp", address: "127.0.0.1:6343"},
		{spec: "netflowv5:unixgram:/tmp/nfv5.sock", enc: "netflowv5", sink: "unixgram", address: "/tmp/nfv5.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			enc, sink, err := parseOutputSpec(tt.spec)
			if err != nil {
				t.Fatalf("parseOutputSpec returned error: %v", err)
			}
			if enc.Type != tt.enc || sink.Type != tt.sink || sink.Path != tt.path || sink.Address != tt.address {
				t.Fatalf("unexpected output for %q: encoder=%#v sink=%#v", tt.spec, enc, sink)
			}
		})
	}
}

func TestLoadFromFlagsGeneratedAggregation(t *testing.T) {
	cfg, _, err := LoadFromFlags(&FlagConfig{
		Inputs:    []string{"udp:[::1]:6343:flow"},
		Output:    "ipfix:udp:192.168.0.2:2055",
		OutputSet: true,
		Aggregate: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if len(cfg.Aggregators) != 2 {
		t.Fatalf("expected 2 generated aggregators, got %d", len(cfg.Aggregators))
	}
	for i, family := range []string{"ipv4", "ipv6"} {
		agg := cfg.Aggregators[i]
		if len(agg.Match) != 1 || agg.Match["ip_family"] != family {
			t.Fatalf("unexpected match for %s aggregator: %#v", family, agg.Match)
		}
		if !contains(agg.Current, "sampling_rate") {
			t.Fatalf("expected %s aggregator to keep sampling_rate current field: %#v", family, agg.Current)
		}
		for _, field := range []string{"mpls_label1", "mpls_label2", "mpls_label3"} {
			if !contains(agg.Current, field) {
				t.Fatalf("expected %s aggregator to keep %s: %#v", family, field, agg.Current)
			}
			if _, ok := cfg.Encoder.TFlowData.Catalog[field]; !ok {
				t.Fatalf("expected generated catalog override for %s", field)
			}
		}
	}
	if cfg.Sink.Type != "udp" || cfg.Sink.Address != "192.168.0.2:2055" {
		t.Fatalf("unexpected generated sink: %#v", cfg.Sink)
	}
}

func TestLoadFromFlagsRejectsInvalidCombinations(t *testing.T) {
	tests := []FlagConfig{
		{ConfigPath: "reflow.yaml", GenConf: true},
		{Output: "pcap:udp:127.0.0.1:9000", OutputSet: true},
		{Inputs: []string{"pcap_live:en0:flow"}},
	}
	for _, flags := range tests {
		if _, _, err := LoadFromFlags(&flags); err == nil {
			t.Fatalf("expected LoadFromFlags to reject %#v", flags)
		}
	}
}

func TestGeneratedConfigYAMLRoundTrip(t *testing.T) {
	cfg, _, err := LoadFromFlags(&FlagConfig{
		GenConf:   true,
		Aggregate: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	if !strings.Contains(string(raw), "mpls_label1") {
		t.Fatalf("expected generated YAML to include MPLS labels:\n%s", raw)
	}

	var roundTrip Config
	if err := yaml.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if err := roundTrip.setDefaults(generatedConfigPath); err != nil {
		t.Fatalf("round-trip defaults returned error: %v", err)
	}
	if len(roundTrip.Aggregators) != 2 {
		t.Fatalf("expected 2 round-tripped aggregators, got %d", len(roundTrip.Aggregators))
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
