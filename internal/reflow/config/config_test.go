package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSetsAggregatorDefaultsAndLoadsIPFIXFields(t *testing.T) {
	dir := t.TempDir()

	ipfixPath := filepath.Join(dir, "ipfix-fields.yaml")
	if err := os.WriteFile(ipfixPath, []byte(`
fields:
  bytes:
    name: octetDeltaCount
    id: 1
    length: 8
    type: unsigned64
    netflow_v9_id: 1
  custom_counter:
    name: customCounter
    id: 1000
    pen: 32473
    enterprise_scoped: true
    length: 8
    type: unsigned64
`), 0o644); err != nil {
		t.Fatalf("write ipfix fields: %v", err)
	}

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
source:
  network: udp
  address: ":18081"
  type: json
  json:
    flavor: reflow

processor:
  type: builtin

aggregator:
  type: window
  flush_interval_ms: 5000

ipfix:
  fields_path: ipfix-fields.yaml
  overrides:
    custom_counter:
      name: customCounterOverride
      id: 2000
      pen: 64512
      enterprise_scoped: true
      length: 8
      type: unsigned64

encoder:
  type: json

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Aggregator.Type != "window" {
		t.Fatalf("expected aggregator.type=window, got %q", cfg.Aggregator.Type)
	}
	if len(cfg.Aggregator.Sum) == 0 || cfg.Aggregator.Sum[0] != "bytes" {
		t.Fatalf("expected default sum fields to include bytes, got %#v", cfg.Aggregator.Sum)
	}
	if len(cfg.Aggregator.First) == 0 || cfg.Aggregator.First[0] != "agent_ip" {
		t.Fatalf("expected default first fields to include agent_ip, got %#v", cfg.Aggregator.First)
	}
	if len(cfg.Aggregator.Current) == 0 || cfg.Aggregator.Current[0] != "agent_ip" {
		t.Fatalf("expected default current fields to include agent_ip, got %#v", cfg.Aggregator.Current)
	}
	custom := cfg.IPFIX.Fields["custom_counter"]
	if custom.ID != 2000 || custom.PEN != 64512 {
		t.Fatalf("expected override for custom_counter to win, got %#v", custom)
	}
	if cfg.IPFIX.Fields["bytes"].ID != 1 {
		t.Fatalf("expected bytes field definition to be loaded from external catalog")
	}
}

func TestLoadSupportsPeriodicAggregator(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
source:
  network: udp
  address: ":18081"
  type: json
  json:
    flavor: reflow

processor:
  type: builtin

aggregator:
  type: periodic

encoder:
  type: json

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Aggregator.PeriodicInterval != 30000 {
		t.Fatalf("expected periodic_interval_ms default 30000, got %d", cfg.Aggregator.PeriodicInterval)
	}
	if len(cfg.Aggregator.Sum) == 0 || len(cfg.Aggregator.First) == 0 || len(cfg.Aggregator.Current) == 0 {
		t.Fatalf("expected periodic aggregator defaults to be populated, got sum=%#v first=%#v current=%#v", cfg.Aggregator.Sum, cfg.Aggregator.First, cfg.Aggregator.Current)
	}
}
