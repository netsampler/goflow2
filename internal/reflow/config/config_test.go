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

aggregators:
  - enabled: true
    window:
      idle_flush_after_ms: 5000
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

	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected 1 aggregator, got %d", len(cfg.Aggregators))
	}
	if !cfg.Aggregators[0].Enabled {
		t.Fatalf("expected aggregators[0].enabled=true")
	}
	if cfg.Aggregators[0].Window.IdleFlushAfter != 5000 {
		t.Fatalf("expected aggregators[0].window.idle_flush_after_ms=5000, got %d", cfg.Aggregators[0].Window.IdleFlushAfter)
	}
	if len(cfg.Aggregators[0].Sum) == 0 || cfg.Aggregators[0].Sum[0] != "bytes" {
		t.Fatalf("expected default sum fields to include bytes, got %#v", cfg.Aggregators[0].Sum)
	}
	if len(cfg.Aggregators[0].First) == 0 || cfg.Aggregators[0].First[0] != "agent_ip" {
		t.Fatalf("expected default first fields to include agent_ip, got %#v", cfg.Aggregators[0].First)
	}
	if len(cfg.Aggregators[0].Current) == 0 || cfg.Aggregators[0].Current[0] != "agent_ip" {
		t.Fatalf("expected default current fields to include agent_ip, got %#v", cfg.Aggregators[0].Current)
	}
	custom := cfg.Encoder.TFlowData.Catalog["custom_counter"]
	if custom.ID != 2000 || custom.PEN != 64512 {
		t.Fatalf("expected override for custom_counter to win, got %#v", custom)
	}
	if cfg.Encoder.TFlowData.Catalog["bytes"].ID != 1 {
		t.Fatalf("expected bytes field definition to be loaded from external catalog")
	}
	if cfg.Aggregators[0].TemplateID != 256 {
		t.Fatalf("expected aggregators[0].template_id=256, got %d", cfg.Aggregators[0].TemplateID)
	}
	if cfg.Aggregators[0].StaticFields["exporter_name"] != "reflow-test" {
		t.Fatalf("expected static field exporter_name to be loaded, got %#v", cfg.Aggregators[0].StaticFields["exporter_name"])
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

aggregators:
  - enabled: true
    periodic:
      every_ms: 60000

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

	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected 1 aggregator, got %d", len(cfg.Aggregators))
	}
	if cfg.Aggregators[0].Periodic.Every != 60000 {
		t.Fatalf("expected periodic.every_ms=60000, got %d", cfg.Aggregators[0].Periodic.Every)
	}
	if len(cfg.Aggregators[0].Sum) == 0 || len(cfg.Aggregators[0].First) == 0 || len(cfg.Aggregators[0].Current) == 0 {
		t.Fatalf("expected aggregation defaults to be populated, got sum=%#v first=%#v current=%#v", cfg.Aggregators[0].Sum, cfg.Aggregators[0].First, cfg.Aggregators[0].Current)
	}
	if cfg.Aggregators[0].Stream != "flow_data" {
		t.Fatalf("expected default stream=flow_data, got %q", cfg.Aggregators[0].Stream)
	}
}

func TestLoadRejectsAggregatorWithoutExportTrigger(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
source:
  network: udp
  address: ":18081"
  type: json

processor:
  type: builtin

aggregators:
  - enabled: true

encoder:
  type: json

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected Load to reject aggregator without export trigger")
	}
}

func TestLoadSupportsAggregatorList(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
source:
  network: udp
  address: ":18081"
  type: json

processor:
  type: builtin

aggregators:
  - enabled: true
    stream: agg_samples
    periodic:
      every_ms: 1000
    match:
      record_kind: packet
  - enabled: true
    stream: agg_counters
    periodic:
      every_ms: 1000
    match:
      record_kind: interface_counter
    current:
      - if_in_octets

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

	if len(cfg.Aggregators) != 2 {
		t.Fatalf("expected 2 aggregators, got %d", len(cfg.Aggregators))
	}
	if cfg.Aggregators[0].Stream != "agg_samples" {
		t.Fatalf("expected first stream agg_samples, got %q", cfg.Aggregators[0].Stream)
	}
	if cfg.Aggregators[0].Match["record_kind"] != "packet" {
		t.Fatalf("expected first match record_kind=packet, got %#v", cfg.Aggregators[0].Match)
	}
	if cfg.Aggregators[1].Stream != "agg_counters" {
		t.Fatalf("expected second stream agg_counters, got %q", cfg.Aggregators[1].Stream)
	}
	if cfg.Aggregators[1].Match["record_kind"] != "interface_counter" {
		t.Fatalf("expected second match record_kind=interface_counter, got %#v", cfg.Aggregators[1].Match)
	}
}

func TestLoadSupportsJSONDropFields(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
source:
  network: udp
  address: ":18081"
  type: json

processor:
  type: builtin

encoder:
  type: json
  json:
    flavor: canonical
    drop_fields:
      - header_data
      - payload

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if len(cfg.Encoder.JSON.DropFields) != 2 {
		t.Fatalf("expected 2 json.drop_fields entries, got %#v", cfg.Encoder.JSON.DropFields)
	}
	if cfg.Encoder.JSON.DropFields[0] != "header_data" || cfg.Encoder.JSON.DropFields[1] != "payload" {
		t.Fatalf("unexpected json.drop_fields contents: %#v", cfg.Encoder.JSON.DropFields)
	}
}

func TestLoadSupportsSFlowCounterFormat(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
source:
  network: udp
  address: ":18081"
  type: json

processor:
  type: builtin

encoder:
  type: sflow
  sflow:
    counter_format: expanded

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Encoder.SFlow.CounterFormat != "expanded" {
		t.Fatalf("expected sflow.counter_format=expanded, got %#v", cfg.Encoder.SFlow.CounterFormat)
	}
}
