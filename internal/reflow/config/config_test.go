package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSetsAggregatorDefaultsAndLoadsFlowDataFields(t *testing.T) {
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
  enabled: true
  reset_interval_ms: 5000
  template_id: 256
  static_fields:
    exporter_name: reflow-test

encoder:
  type: json
  tflow_data:
    fields_path: ipfix-fields.yaml
    overrides:
      custom_counter:
        name: customCounterOverride
        id: 2000
        pen: 64512
        enterprise_scoped: true
        length: 8
        type: unsigned64

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if !cfg.Aggregator.Enabled {
		t.Fatalf("expected aggregator.enabled=true")
	}
	if cfg.Aggregator.ResetInterval != 5000 {
		t.Fatalf("expected aggregator.reset_interval_ms=5000, got %d", cfg.Aggregator.ResetInterval)
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
	custom := cfg.Encoder.TFlowData.Catalog["custom_counter"]
	if custom.ID != 2000 || custom.PEN != 64512 {
		t.Fatalf("expected override for custom_counter to win, got %#v", custom)
	}
	if cfg.Encoder.TFlowData.Catalog["bytes"].ID != 1 {
		t.Fatalf("expected bytes field definition to be loaded from external catalog")
	}
	if cfg.Aggregator.TemplateID != 256 {
		t.Fatalf("expected aggregator.template_id=256, got %d", cfg.Aggregator.TemplateID)
	}
	if cfg.Aggregator.StaticFields["exporter_name"] != "reflow-test" {
		t.Fatalf("expected static field exporter_name to be loaded, got %#v", cfg.Aggregator.StaticFields["exporter_name"])
	}
}

func TestLoadSupportsAccumulativeAggregatorDefaults(t *testing.T) {
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
  enabled: true

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

	if cfg.Aggregator.PeriodicInterval != 60000 {
		t.Fatalf("expected periodic_interval_ms default 60000, got %d", cfg.Aggregator.PeriodicInterval)
	}
	if cfg.Aggregator.ResetInterval != 0 {
		t.Fatalf("expected reset_interval_ms default 0, got %d", cfg.Aggregator.ResetInterval)
	}
	if len(cfg.Aggregator.Sum) == 0 || len(cfg.Aggregator.First) == 0 || len(cfg.Aggregator.Current) == 0 {
		t.Fatalf("expected aggregation defaults to be populated, got sum=%#v first=%#v current=%#v", cfg.Aggregator.Sum, cfg.Aggregator.First, cfg.Aggregator.Current)
	}
}
