package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestLoadSetsAggregatorDefaultsAndLoadsFlowDataFields(t *testing.T) {
	dir := t.TempDir()

	ipfixPath := filepath.Join(dir, "fields.yaml")
	if err := os.WriteFile(ipfixPath, []byte(`
fields:
  bytes: 1:8:u64:delta
  custom_counter:
    name: customCounter
    id: 1000
    pen: 32473
    enterprise_scoped: true
    length: 8
    type: unsigned64
  compact_enterprise: 4000:4:u32[pen=64513]
`), 0o644); err != nil {
		t.Fatalf("write ipfix fields: %v", err)
	}

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json
    json:
      flavor: reflow

processor:
  type: builtin
  builtin:
    packet_decoder:
      decode_beyond_l4: false
      encapsulations:
        gre:
          enabled: true
        ipip:
          enabled: true
        vxlan:
          enabled: false
          ports: [4789, 4790]
        geneve:
          enabled: true
          ports: [6081]
        l2tp:
          enabled: true
          ports: [1701]
        gtpu:
          enabled: true
          ports: [2152]
        pppoe:
          enabled: false
    aggregation_helpers:
      mpls_labels: 3
      ip_layers: 2

aggregators:
  - window:
      idle_flush_after_ms: 5000
    template_id: 256
    static_fields:
      exporter_name: reflow-test

encoder:
  type: json
  templated_flow:
    data:
      fields_path: fields.yaml
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

	if len(cfg.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(cfg.Sources))
	}
	if cfg.Sources[0].Address != ":18081" {
		t.Fatalf("expected sources[0].address=:18081, got %q", cfg.Sources[0].Address)
	}
	decoder := cfg.Processor.Builtin.PacketDecoder
	if decoder.DecodeBeyondL4 == nil || *decoder.DecodeBeyondL4 {
		t.Fatalf("expected processor packet decoder decode_beyond_l4=false, got %#v", decoder.DecodeBeyondL4)
	}
	if decoder.Encapsulations.GRE.Enabled == nil || !*decoder.Encapsulations.GRE.Enabled {
		t.Fatalf("expected GRE encapsulation enabled=true, got %#v", decoder.Encapsulations.GRE.Enabled)
	}
	if decoder.Encapsulations.VXLAN.Enabled == nil || *decoder.Encapsulations.VXLAN.Enabled {
		t.Fatalf("expected VXLAN encapsulation enabled=false, got %#v", decoder.Encapsulations.VXLAN.Enabled)
	}
	if len(decoder.Encapsulations.VXLAN.Ports) != 2 || decoder.Encapsulations.VXLAN.Ports[1] != 4790 {
		t.Fatalf("expected VXLAN ports [4789 4790], got %#v", decoder.Encapsulations.VXLAN.Ports)
	}
	if len(decoder.Encapsulations.L2TP.Ports) != 1 || decoder.Encapsulations.L2TP.Ports[0] != 1701 {
		t.Fatalf("expected L2TP ports [1701], got %#v", decoder.Encapsulations.L2TP.Ports)
	}
	if len(decoder.Encapsulations.GTPU.Ports) != 1 || decoder.Encapsulations.GTPU.Ports[0] != 2152 {
		t.Fatalf("expected GTP-U ports [2152], got %#v", decoder.Encapsulations.GTPU.Ports)
	}
	if cfg.Processor.Builtin.AggregationHelpers.MPLSLabels != 3 {
		t.Fatalf("expected aggregation helper mpls_labels=3, got %d", cfg.Processor.Builtin.AggregationHelpers.MPLSLabels)
	}
	if cfg.Processor.Builtin.AggregationHelpers.IPLayers != 2 {
		t.Fatalf("expected aggregation helper ip_layers=2, got %d", cfg.Processor.Builtin.AggregationHelpers.IPLayers)
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected 1 aggregator, got %d", len(cfg.Aggregators))
	}
	if cfg.Aggregators[0].Window.IdleFlushAfter != 5000 {
		t.Fatalf("expected aggregators[0].window.idle_flush_after_ms=5000, got %d", cfg.Aggregators[0].Window.IdleFlushAfter)
	}
	if !cfg.Aggregators[0].Passthrough {
		t.Fatalf("expected aggregators[0] without aggregation fields to use pass-through schema mode")
	}
	if len(cfg.Aggregators[0].Sum) != 0 {
		t.Fatalf("expected sum fields to default empty, got %#v", cfg.Aggregators[0].Sum)
	}
	if len(cfg.Aggregators[0].First) != 0 {
		t.Fatalf("expected first fields to default empty, got %#v", cfg.Aggregators[0].First)
	}
	if len(cfg.Aggregators[0].Current) != 0 {
		t.Fatalf("expected current fields to default empty, got %#v", cfg.Aggregators[0].Current)
	}
	custom := cfg.Encoder.TemplatedFlow.Data.Catalog["custom_counter"]
	if custom.ID != 2000 || custom.PEN != 64512 {
		t.Fatalf("expected override for custom_counter to win, got %#v", custom)
	}
	if cfg.Encoder.TemplatedFlow.Data.Catalog["bytes"].ID != 1 {
		t.Fatalf("expected bytes field definition to be loaded from external catalog")
	}
	compact := cfg.Encoder.TemplatedFlow.Data.Catalog["compact_enterprise"]
	if compact.ID != 4000 || compact.Length != 4 || compact.Type != "unsigned32" || compact.PEN != 64513 || !compact.EnterpriseScoped {
		t.Fatalf("expected compact enterprise field to load, got %#v", compact)
	}
	if cfg.Aggregators[0].TemplateID != 256 {
		t.Fatalf("expected aggregators[0].template_id=256, got %d", cfg.Aggregators[0].TemplateID)
	}
	if cfg.Aggregators[0].StaticFields["exporter_name"] != "reflow-test" {
		t.Fatalf("expected static field exporter_name to be loaded, got %#v", cfg.Aggregators[0].StaticFields["exporter_name"])
	}
}

func TestLoadUsesEmbeddedFlowDataCatalogWhenFieldsPathEmpty(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: flow

processor:
  type: builtin

encoder:
  type: ipfix
  templated_flow:
    data:
      overrides:
        custom_counter:
          name: customCounter
          id: 2000
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
	if cfg.Encoder.TemplatedFlow.Data.FieldsPath != "" {
		t.Fatalf("expected empty fields_path to keep using embedded catalog, got %q", cfg.Encoder.TemplatedFlow.Data.FieldsPath)
	}
	if cfg.Encoder.TemplatedFlow.Data.Catalog["bytes"].ID != 1 {
		t.Fatalf("expected bytes field from embedded catalog, got %#v", cfg.Encoder.TemplatedFlow.Data.Catalog["bytes"])
	}
	if cfg.Encoder.TemplatedFlow.Data.Catalog["mpls_label3"].ID != 72 {
		t.Fatalf("expected mpls_label3 field from embedded catalog, got %#v", cfg.Encoder.TemplatedFlow.Data.Catalog["mpls_label3"])
	}
	if cfg.Encoder.TemplatedFlow.Data.Catalog["agent_ip"].ID != 130 {
		t.Fatalf("expected agent_ip field from embedded catalog, got %#v", cfg.Encoder.TemplatedFlow.Data.Catalog["agent_ip"])
	}
	if cfg.Encoder.TemplatedFlow.Data.Catalog["sample_pool"].ID != 310 {
		t.Fatalf("expected sample_pool field from embedded catalog, got %#v", cfg.Encoder.TemplatedFlow.Data.Catalog["sample_pool"])
	}
	if cfg.Encoder.TemplatedFlow.Data.Catalog["src_mac"].Type != "macAddress" {
		t.Fatalf("expected src_mac macAddress field from embedded catalog, got %#v", cfg.Encoder.TemplatedFlow.Data.Catalog["src_mac"])
	}
	if cfg.Encoder.TemplatedFlow.Data.Catalog["custom_counter"].ID != 2000 {
		t.Fatalf("expected override to merge over embedded catalog, got %#v", cfg.Encoder.TemplatedFlow.Data.Catalog["custom_counter"])
	}

	emptyCatalog := filepath.Join(dir, "empty-fields.yaml")
	if err := os.WriteFile(emptyCatalog, nil, 0o644); err != nil {
		t.Fatalf("write empty catalog: %v", err)
	}
	emptyPathCfg := filepath.Join(dir, "empty-file.yaml")
	if err := os.WriteFile(emptyPathCfg, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: flow

processor:
  type: builtin

encoder:
  type: ipfix
  templated_flow:
    data:
      fields_path: empty-fields.yaml

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write empty-file config: %v", err)
	}
	emptyFile, err := Load(emptyPathCfg)
	if err != nil {
		t.Fatalf("Load empty file returned error: %v", err)
	}
	if emptyFile.Encoder.TemplatedFlow.Data.Catalog["bytes"].ID != 1 {
		t.Fatalf("expected empty catalog file to use embedded bytes field, got %#v", emptyFile.Encoder.TemplatedFlow.Data.Catalog["bytes"])
	}
}

func TestLoadSupportsAccumulativeAggregatorWithExplicitFields(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json
    json:
      flavor: reflow

processor:
  type: builtin

aggregators:
  - periodic:
      every_ms: 60000
    fields:
      - key:agent_ip
      - current:sampling_rate

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
	if cfg.Aggregators[0].Passthrough {
		t.Fatalf("expected current field with export trigger to force aggregate mode")
	}
	if len(cfg.Aggregators[0].KeyFields) != 1 || cfg.Aggregators[0].KeyFields[0] != "agent_ip" {
		t.Fatalf("expected key field agent_ip, got %#v", cfg.Aggregators[0].KeyFields)
	}
	if len(cfg.Aggregators[0].Current) != 1 || cfg.Aggregators[0].Current[0] != "sampling_rate" {
		t.Fatalf("expected current field sampling_rate, got %#v", cfg.Aggregators[0].Current)
	}
	if cfg.Aggregators[0].Stream != "flow_data" {
		t.Fatalf("expected default stream=flow_data, got %q", cfg.Aggregators[0].Stream)
	}
}

func TestLoadParsesAggregatorFieldDSL(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json

processor:
  type: builtin

aggregators:
  - fields:
      - key:src_addr
      - key:dst_addr
      - current:tenant_id
      - static:exporter_name:edge-a

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
	agg := cfg.Aggregators[0]
	if !agg.Passthrough {
		t.Fatalf("expected key/current/static fields without export triggers to use pass-through schema mode")
	}
	if len(agg.Fields) != 4 {
		t.Fatalf("expected 4 parsed fields, got %#v", agg.Fields)
	}
	if got := agg.Fields[0]; got.Role != "key" || got.Name != "src_addr" {
		t.Fatalf("unexpected first field: %#v", got)
	}
	if got := agg.Fields[2]; got.Role != "current" || got.Name != "tenant_id" {
		t.Fatalf("unexpected tenant field: %#v", got)
	}
	if agg.StaticFields["exporter_name"] != "edge-a" {
		t.Fatalf("expected static exporter_name edge-a, got %#v", agg.StaticFields["exporter_name"])
	}
}

func TestLoadRejectsPlainAggregatorFieldRole(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json

processor:
  type: builtin

aggregators:
  - fields:
      - key:src_addr
      - field:bytes

encoder:
  type: json

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected Load to reject plain field role")
	}
}

func TestLoadRejectsAggregatorFieldModifier(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json

processor:
  type: builtin

aggregators:
  - fields:
      - key:src_addr:4

encoder:
  type: json

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected Load to reject field modifier")
	}
}

func TestLoadSupportsStreamPcapAndPcapSinkDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: stream
    address: "-"
    type: pcap

processor:
  type: builtin

encoder:
  type: pcap

sink:
  type: file
  path: out.pcap
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Sources[0].Address != "-" {
		t.Fatalf("expected stream stdin address, got %q", cfg.Sources[0].Address)
	}
	if cfg.Encoder.Pcap.PacketSource != "auto" {
		t.Fatalf("expected default packet_source auto, got %q", cfg.Encoder.Pcap.PacketSource)
	}
	if cfg.Encoder.Pcap.LinkType != "ethernet" {
		t.Fatalf("expected default pcap link_type ethernet, got %q", cfg.Encoder.Pcap.LinkType)
	}
	if cfg.Encoder.Pcap.SnapLen != 65535 {
		t.Fatalf("expected default pcap snaplen 65535, got %d", cfg.Encoder.Pcap.SnapLen)
	}
	if cfg.Sink.Framing != "none" {
		t.Fatalf("expected pcap sink framing none, got %q", cfg.Sink.Framing)
	}
	if cfg.Sink.Mode != "truncate" {
		t.Fatalf("expected pcap file sink mode truncate, got %q", cfg.Sink.Mode)
	}
}

func TestLoadRejectsPcapEncoderWithDatagramSink(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: stream
    address: "-"
    type: pcap

processor:
  type: builtin

encoder:
  type: pcap

sink:
  type: udp
  address: "127.0.0.1:9000"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected pcap encoder with UDP sink to be rejected")
	}
}

func TestLoadRejectsPcapNGEncoderWithDatagramSink(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: stream
    address: "-"
    type: pcapng

processor:
  type: builtin

encoder:
  type: pcapng

sink:
  type: udp
  address: "127.0.0.1:9000"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected pcapng encoder with UDP sink to be rejected")
	}
}

func TestLoadSupportsPcapNGEncoderDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: stream
    address: "-"
    type: pcapng

processor:
  type: builtin

encoder:
  type: pcapng

sink:
  type: file
  path: out.pcapng
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Encoder.Pcap.LinkType != "ethernet" {
		t.Fatalf("expected default pcapng link_type ethernet, got %q", cfg.Encoder.Pcap.LinkType)
	}
	if cfg.Sink.Framing != "none" {
		t.Fatalf("expected pcapng sink framing none, got %q", cfg.Sink.Framing)
	}
	if cfg.Sink.Mode != "truncate" {
		t.Fatalf("expected pcapng file sink mode truncate, got %q", cfg.Sink.Mode)
	}
}

func TestLoadAllowsPassthroughAggregatorWithoutExportTrigger(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json

processor:
  type: builtin

aggregators:
  - fields:
      - key:src_addr
      - current:bytes

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
	if !cfg.Aggregators[0].Passthrough {
		t.Fatalf("expected aggregator without stateful rollup to use pass-through schema mode")
	}
}

func TestLoadSupportsAggregatorList(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json

processor:
  type: builtin

aggregators:
  - stream: agg_samples
    periodic:
      every_ms: 1000
    match:
      record_kind: packet
  - stream: agg_counters
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

func TestGeneratedAggregateConfigUsesFieldDSL(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{
		Aggregate: true,
		GenConf:   true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	out := string(raw)
	for _, want := range []string{
		"fields:",
		"- key:src_addr",
		"- sum:bytes",
		"- first:agent_ip",
		"- current:end_time_unix",
		"template_id: 258",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected generated config to contain %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{
		"key_fields:",
		"static_fields:",
		"reset_interval_ms:",
		"periodic_interval_ms:",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("expected generated config not to contain %q:\n%s", unwanted, out)
		}
	}
}

func TestLoadSupportsJSONDropFields(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
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
sources:
  - network: udp
    address: ":18081"
    type: json

processor:
  type: builtin

encoder:
  type: sflow
  sflow:
    counter_format: expanded
    use_metadata_sequence_number: true

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
	if !cfg.Encoder.SFlow.UseMetadataSequenceNumber {
		t.Fatalf("expected sflow.use_metadata_sequence_number=true")
	}
}

func TestLoadSupportsProtobufFlavor(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json

processor:
  type: builtin

encoder:
  type: protobuf
  protobuf:
    flavor: goflow2v2
    length_prefixed: true

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Encoder.Protobuf.Flavor != "goflow2v2" {
		t.Fatalf("expected protobuf.flavor=goflow2v2, got %#v", cfg.Encoder.Protobuf.Flavor)
	}
	if !cfg.Encoder.Protobuf.LengthPrefixed {
		t.Fatalf("expected protobuf.length_prefixed=true")
	}
}

func TestLoadSupportsTemplatedFlowEncoderSubsection(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: flow

processor:
  type: builtin

encoder:
  type: ipfix
  templated_flow:
    template_base_id: 300
    options_template_base_id: 1300
    observation_domain_id: 42
    template_refresh_ms: 70000
    options_refresh_ms: 40000

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Encoder.TemplatedFlow.TemplateBaseID != 300 {
		t.Fatalf("expected templated_flow.template_base_id=300, got %d", cfg.Encoder.TemplatedFlow.TemplateBaseID)
	}
	if cfg.Encoder.TemplatedFlow.OptionsTemplateBaseID != 1300 {
		t.Fatalf("expected templated_flow.options_template_base_id=1300, got %d", cfg.Encoder.TemplatedFlow.OptionsTemplateBaseID)
	}
	if cfg.Encoder.TemplatedFlow.ObservationDomainID != 42 {
		t.Fatalf("expected templated_flow.observation_domain_id=42, got %d", cfg.Encoder.TemplatedFlow.ObservationDomainID)
	}
	if cfg.Encoder.TemplatedFlow.TemplateRefresh != 70000 {
		t.Fatalf("expected templated_flow.template_refresh_ms=70000, got %d", cfg.Encoder.TemplatedFlow.TemplateRefresh)
	}
	if cfg.Encoder.TemplatedFlow.OptionsRefresh != 40000 {
		t.Fatalf("expected templated_flow.options_refresh_ms=40000, got %d", cfg.Encoder.TemplatedFlow.OptionsRefresh)
	}
}

func TestLoadRejectsMissingSources(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
processor:
  type: builtin

encoder:
  type: json

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(cfgPath); err == nil {
		t.Fatalf("expected Load to reject missing sources")
	}
}

func TestHelperOptionsTextListsInputAndOutputExamples(t *testing.T) {
	text := HelperOptionsText()
	for _, want := range []string{
		"udp::6343:flow",
		"ebpf:eth0:bytes",
		"pcap_live:en0:bytes",
		"json:stdout",
		"ipfix:udp:127.0.0.1:4739",
		"pcap:stdout",
		"stream:<path-or-stdin>:json",
		"encoders: json, protobuf, sflow, ipfix, netflowv9, netflowv5, pcap, pcapng",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected helper options to contain %q, got:\n%s", want, text)
		}
	}
}

func TestGeneratedConfigYAMLUsesFalseForPacketDecoderBooleans(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{GenConf: true})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config mode")
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, ": null") {
		t.Fatalf("expected generated config to avoid null booleans, got:\n%s", text)
	}
	for _, want := range []string{
		"decode_beyond_l4: false",
		"enabled: false",
		"template_base_id: 256",
		"options_template_base_id: 1024",
		"template_refresh_ms: 60000",
		"options_refresh_ms: 30000",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected generated config to contain %q, got:\n%s", want, text)
		}
	}
}

func TestLoadSupportsEBPFSourceDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: ebpf
    interface: eth0

processor:
  type: builtin

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
	if cfg.Sources[0].Type != "bytes" {
		t.Fatalf("expected ebpf source type bytes, got %q", cfg.Sources[0].Type)
	}
	if cfg.Sources[0].Address != "eth0" {
		t.Fatalf("expected ebpf address eth0, got %q", cfg.Sources[0].Address)
	}
	if cfg.Sources[0].SnapLen != 65535 {
		t.Fatalf("expected ebpf snaplen 65535, got %d", cfg.Sources[0].SnapLen)
	}
	if cfg.Sources[0].SampleEvery != 1 {
		t.Fatalf("expected ebpf sample_every 1, got %d", cfg.Sources[0].SampleEvery)
	}
}

func TestParseInputSpecSupportsEBPF(t *testing.T) {
	src, err := parseInputSpec("ebpf:eth0:bytes")
	if err != nil {
		t.Fatalf("parseInputSpec returned error: %v", err)
	}
	if src.Network != "ebpf" || src.Interface != "eth0" || src.Type != "bytes" || src.Address != "" {
		t.Fatalf("unexpected ebpf source config: %#v", src)
	}
}
