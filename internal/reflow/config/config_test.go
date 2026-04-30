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
          protocols: [47]
        ipip:
          enabled: true
          protocols: [4]
        ip6ip:
          enabled: true
          protocols: [41]
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
	if len(decoder.Encapsulations.GRE.Protocols) != 1 || decoder.Encapsulations.GRE.Protocols[0] != 47 {
		t.Fatalf("expected GRE protocols [47], got %#v", decoder.Encapsulations.GRE.Protocols)
	}
	if len(decoder.Encapsulations.IPIP.Protocols) != 1 || decoder.Encapsulations.IPIP.Protocols[0] != 4 {
		t.Fatalf("expected IPIP protocols [4], got %#v", decoder.Encapsulations.IPIP.Protocols)
	}
	if len(decoder.Encapsulations.IP6IP.Protocols) != 1 || decoder.Encapsulations.IP6IP.Protocols[0] != 41 {
		t.Fatalf("expected IP6IP protocols [41], got %#v", decoder.Encapsulations.IP6IP.Protocols)
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
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected 1 aggregator, got %d", len(cfg.Aggregators))
	}
	if !cfg.Aggregators[0].Enabled {
		t.Fatalf("expected aggregators[0].enabled=true")
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
  - enabled: true
    periodic:
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
		t.Fatalf("expected current field to force aggregate mode")
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
  - enabled: true
    fields:
      - key:src_addr:4
      - key:dst_addr:4
      - field:tenant_id
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
		t.Fatalf("expected no aggregation roles to use pass-through schema mode")
	}
	if len(agg.Fields) != 4 {
		t.Fatalf("expected 4 parsed fields, got %#v", agg.Fields)
	}
	if got := agg.Fields[0]; got.Role != "key" || got.Name != "src_addr" || got.Modifier != "4" {
		t.Fatalf("unexpected first field: %#v", got)
	}
	if got := agg.Fields[2]; got.Role != "field" || got.Name != "tenant_id" || got.Modifier != "" {
		t.Fatalf("unexpected tenant field: %#v", got)
	}
	if agg.StaticFields["exporter_name"] != "edge-a" {
		t.Fatalf("expected static exporter_name edge-a, got %#v", agg.StaticFields["exporter_name"])
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
  - enabled: true
    fields:
      - key:src_addr
      - field:bytes

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
		t.Fatalf("expected aggregator without aggregation roles to use pass-through schema mode")
	}
}

func TestLoadAllowsDisabledAggregatorWithoutExportTrigger(t *testing.T) {
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
  - enabled: false

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
	if cfg.Aggregators[0].Enabled {
		t.Fatalf("expected disabled aggregator to remain disabled")
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
