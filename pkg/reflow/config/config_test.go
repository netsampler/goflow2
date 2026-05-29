package config

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
    nat:
      swap_pre_post: true

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

	decoder := cfg.Processor.Builtin.PacketDecoder
	if decoder.DecodeBeyondL4 == nil || *decoder.DecodeBeyondL4 {
		t.Fatalf("expected processor packet decoder decode_beyond_l4=false, got %#v", decoder.DecodeBeyondL4)
	}
	if decoder.Encapsulations.VXLAN.Enabled == nil || *decoder.Encapsulations.VXLAN.Enabled {
		t.Fatalf("expected VXLAN encapsulation enabled=false, got %#v", decoder.Encapsulations.VXLAN.Enabled)
	}
	if len(decoder.Encapsulations.VXLAN.Ports) != 2 || decoder.Encapsulations.VXLAN.Ports[1] != 4790 {
		t.Fatalf("expected VXLAN ports [4789 4790], got %#v", decoder.Encapsulations.VXLAN.Ports)
	}
	if cfg.Processor.Builtin.AggregationHelpers.MPLSLabels != 3 {
		t.Fatalf("expected aggregation helper mpls_labels=3, got %d", cfg.Processor.Builtin.AggregationHelpers.MPLSLabels)
	}
	if !cfg.Processor.Builtin.NAT.SwapPrePost {
		t.Fatalf("expected processor.builtin.nat.swap_pre_post=true")
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
	custom := cfg.Encoder.TemplatedFlow.Data.Catalog["custom_counter"]
	if custom.ID != 2000 || custom.PEN != 64512 {
		t.Fatalf("expected override for custom_counter to win, got %#v", custom)
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
	mpls := cfg.Encoder.TemplatedFlow.Data.Catalog["mpls_label_stack_section_3"]
	if mpls.ID != 72 || mpls.Length != 3 || mpls.Type != "bytes" {
		t.Fatalf("expected mpls_label_stack_section_3 bytes/3 field from embedded catalog, got %#v", mpls)
	}
	headerData := cfg.Encoder.TemplatedFlow.Data.Catalog["header_data"]
	if headerData.ID != 315 || headerData.Length != 65535 || headerData.Type != "bytes" {
		t.Fatalf("expected header_data variable dataLinkFrameSection field from embedded catalog, got %#v", headerData)
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
	if got := emptyFile.Encoder.TemplatedFlow.Data.Catalog["agent_ipv6"]; got.ID != 131 || got.Length != 16 || got.Type != "ipv6Address" {
		t.Fatalf("expected embedded agent_ipv6 exporter IPv6 field, got %#v", got)
	}
}

func TestLoadSupportsSharedTemplatedFieldsCatalog(t *testing.T) {
	dir := t.TempDir()
	fieldsPath := filepath.Join(dir, "shared-fields.yaml")
	if err := os.WriteFile(fieldsPath, []byte(`
fields:
  bytes: 1:8:u64:delta
  custom_counter:
    name: customCounter
    id: 1000
    pen: 32473
    enterprise_scoped: true
    length: 8
    type: unsigned64
`), 0o644); err != nil {
		t.Fatalf("write fields: %v", err)
	}
	exportFieldsPath := filepath.Join(dir, "export-fields.yaml")
	if err := os.WriteFile(exportFieldsPath, []byte(`
fields:
  file_export_only:
    id: 3000
    length: 4
    type: unsigned32
`), 0o644); err != nil {
		t.Fatalf("write export fields: %v", err)
	}
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: flow

processor:
  type: builtin

templated_fields:
  fields_path: shared-fields.yaml
  overrides:
    custom_counter:
      id: 1001
      pen: 32473
      enterprise_scoped: true
      length: 8
      type: unsigned64

encoder:
  type: ipfix
  templated_flow:
    data:
      fields_path: export-fields.yaml
      fields:
        - bytes
      overrides:
        export_only:
          id: 2000
          length: 4
          type: unsigned32

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	shared := cfg.TemplatedFields.Catalog["custom_counter"]
	if shared.ID != 1001 || shared.PEN != 32473 {
		t.Fatalf("expected top-level override to apply to shared catalog, got %#v", shared)
	}
	if _, ok := cfg.TemplatedFields.Catalog["export_only"]; ok {
		t.Fatalf("expected encoder-only override not to change shared decode catalog")
	}
	if _, ok := cfg.TemplatedFields.Catalog["file_export_only"]; ok {
		t.Fatalf("expected encoder-only fields_path not to change shared decode catalog")
	}
	if cfg.Encoder.TemplatedFlow.Data.Catalog["file_export_only"].ID != 3000 {
		t.Fatalf("expected encoder-only fields_path in export catalog, got %#v", cfg.Encoder.TemplatedFlow.Data.Catalog["file_export_only"])
	}
	if len(cfg.Encoder.TemplatedFlow.Data.Select) != 1 || cfg.Encoder.TemplatedFlow.Data.Select[0] != "bytes" {
		t.Fatalf("expected export field selection to remain export-only, got %#v", cfg.Encoder.TemplatedFlow.Data.Select)
	}
}

func TestLoadTreatsExplicitEmptySharedTemplatedFieldsPathAsEmptyCatalog(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: flow

processor:
  type: builtin

templated_fields:
  fields_path: ""
  overrides:
    custom_counter:
      id: 1000
      pen: 32473
      enterprise_scoped: true
      length: 8
      type: unsigned64

encoder:
  type: ipfix

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if _, ok := cfg.TemplatedFields.Catalog["bytes"]; ok {
		t.Fatalf("expected explicit empty templated_fields.fields_path not to load embedded defaults")
	}
	if cfg.TemplatedFields.Catalog["custom_counter"].ID != 1000 {
		t.Fatalf("expected override to remain in shared catalog, got %#v", cfg.TemplatedFields.Catalog["custom_counter"])
	}
}

func TestLoadRejectsDuplicateTemplatedDecodeFields(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: flow

processor:
  type: builtin

templated_fields:
  overrides:
    custom_bytes:
      id: 1
      length: 8
      type: unsigned64

encoder:
  type: ipfix

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := Load(cfgPath); err == nil || !strings.Contains(err.Error(), "duplicate IPFIX decode mapping") {
		t.Fatalf("expected duplicate IPFIX decode mapping error, got %v", err)
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

func TestLoadParsesAggregatorAndField(t *testing.T) {
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
  - periodic:
      every_ms: 1000
    fields:
      - key:src_addr
      - and:tcp_flags

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
	if agg.Passthrough {
		t.Fatalf("expected and field to force aggregate mode")
	}
	if !aggregatorHasField(agg, "and", "tcp_flags") {
		t.Fatalf("expected and:tcp_flags, got %#v", agg.Fields)
	}
	if len(agg.And) != 1 || agg.And[0] != "tcp_flags" {
		t.Fatalf("expected And list to contain tcp_flags, got %#v", agg.And)
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

func TestLoadAssignsSourceIDsByLocalObservationPointOrder(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: stream
    address: first.pcap
    type: pcap
  - network: stream
    address: events.ndjson
    type: json
  - network: stream
    address: second.pcapng
    type: pcapng
  - network: pcap_live
    interface: lo0
    type: bytes
    source_id: 9
  - network: ebpf
    interface: eth0
    type: bytes
processor:
  type: builtin
encoder:
  type: ipfix
sink:
  type: file
  path: out.ipfix
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Sources[0].SourceID == nil || *cfg.Sources[0].SourceID != 0 {
		t.Fatalf("expected first stream pcap source_id 0, got %#v", cfg.Sources[0].SourceID)
	}
	if cfg.Sources[1].SourceID != nil {
		t.Fatalf("expected stream json not to receive source_id, got %#v", cfg.Sources[1].SourceID)
	}
	if cfg.Sources[2].SourceID == nil || *cfg.Sources[2].SourceID != 1 {
		t.Fatalf("expected stream pcapng source_id 1, got %#v", cfg.Sources[2].SourceID)
	}
	if cfg.Sources[3].SourceID == nil || *cfg.Sources[3].SourceID != 9 {
		t.Fatalf("expected explicit pcap_live source_id 9, got %#v", cfg.Sources[3].SourceID)
	}
	if cfg.Sources[4].SourceID == nil || *cfg.Sources[4].SourceID != 10 {
		t.Fatalf("expected ebpf source_id 10 after explicit id, got %#v", cfg.Sources[4].SourceID)
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
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected one generated aggregator, got %d", len(cfg.Aggregators))
	}
	agg := cfg.Aggregators[0]
	if agg.Match["record_kind"] != "packet" {
		t.Fatalf("expected generated aggregator to match packets, got %#v", agg.Match)
	}
	if agg.TemplateID != 256 {
		t.Fatalf("expected generated template_id 256, got %d", agg.TemplateID)
	}
	for _, field := range []struct {
		role string
		name string
	}{
		{"key", "src_addr"},
		{"sum", "bytes"},
		{"and", "tcp_flags"},
		{"current", "src_mac"},
		{"current", "flow_direction"},
		{"current", "agent_ipv6"},
	} {
		if !aggregatorHasField(agg, field.role, field.name) {
			t.Fatalf("expected generated aggregate field %s:%s, got %#v", field.role, field.name, agg.Fields)
		}
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	out := string(raw)
	for _, unwanted := range []string{"key_fields:", "static_fields:", "reset_interval_ms:", "periodic_interval_ms:"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("expected generated config not to contain %q:\n%s", unwanted, out)
		}
	}
}

func TestGeneratedAggregateConfigSupportsCLIParams(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{
		"-agg=idle_flush_after_ms=0,max_flush_after_ms=45000,periodic_every_ms=15000,reset_buckets=true",
		"-genconf",
	}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected generated aggregators, got %d", len(cfg.Aggregators))
	}
	agg := cfg.Aggregators[0]
	if agg.Match["record_kind"] != "packet" {
		t.Fatalf("match = %#v, want record_kind=packet", agg.Match)
	}
	if agg.Window.IdleFlushAfter != 0 {
		t.Fatalf("idle flush = %d", agg.Window.IdleFlushAfter)
	}
	if agg.Window.MaxFlushAfter != 45000 {
		t.Fatalf("max flush = %d", agg.Window.MaxFlushAfter)
	}
	if agg.Periodic.Every != 15000 {
		t.Fatalf("periodic every = %d", agg.Periodic.Every)
	}
	if !agg.Periodic.ResetBuckets {
		t.Fatalf("expected reset_buckets=true")
	}
}

func TestNormalizeAggregateArgsSupportsSpaceSeparatedValue(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "space separated aggregate preset",
			args: []string{"-agg", "passthrough,payload", "-genconf"},
			want: []string{"-agg=passthrough,payload", "-genconf"},
		},
		{
			name: "bare aggregate remains boolean shorthand",
			args: []string{"-agg", "-genconf"},
			want: []string{"-agg", "-genconf"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeAggregateArgs(tt.args)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("NormalizeAggregateArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestGeneratedAggregateConfigSupportsSpaceSeparatedPresets(t *testing.T) {
	tests := []struct {
		name      string
		preset    string
		checkFunc func(t *testing.T, cfg *Config)
	}{
		{
			name:   "passthrough payload",
			preset: "passthrough,payload",
			checkFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				if len(cfg.Aggregators) != 1 {
					t.Fatalf("expected generated aggregator, got %d", len(cfg.Aggregators))
				}
				agg := cfg.Aggregators[0]
				if !agg.Passthrough {
					t.Fatalf("expected passthrough preset to use schema passthrough")
				}
				if !aggregatorHasField(agg, "current", "frame_length") || !aggregatorHasField(agg, "current", "header_data") {
					t.Fatalf("expected payload fields, got %#v", agg.Fields)
				}
			},
		},
		{
			name:   "none",
			preset: "none",
			checkFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				if len(cfg.Aggregators) != 0 {
					t.Fatalf("expected no generated aggregators, got %d", len(cfg.Aggregators))
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			flags, _ := BindFlags(fs)
			if err := fs.Parse(NormalizeAggregateArgs([]string{"-agg", tt.preset, "-genconf"})); err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			cfg, generated, err := LoadFromFlags(flags)
			if err != nil {
				t.Fatalf("LoadFromFlags returned error: %v", err)
			}
			if !generated {
				t.Fatalf("expected generated config")
			}
			tt.checkFunc(t, cfg)
		})
	}
}

func TestGeneratedIPFIXConfigExcludesDataLinkFrameByDefault(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-output=ipfix:udp:127.0.0.1:4739", "-genconf"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	fields := cfg.Encoder.TemplatedFlow.Data.Select
	if len(fields) == 0 {
		t.Fatalf("expected generated ipfix config to select explicit fields")
	}
	for _, name := range []string{"header_data", "nat_src_addr", "mpls_label_stack_section_1"} {
		if slices.Contains(fields, name) {
			t.Fatalf("expected generated ipfix config not to export %s by default: %#v", name, fields)
		}
	}
	for _, name := range []string{"src_addr", "bytes", "source_id", "flow_direction"} {
		if !slices.Contains(fields, name) {
			t.Fatalf("expected generated ipfix config to export %s: %#v", name, fields)
		}
	}
}

func TestGeneratedTemplatedFlowPickerOrdersAgentFields(t *testing.T) {
	agentIP := slices.Index(generatedTemplatedFlowFields, "agent_ip")
	agentIPv6 := slices.Index(generatedTemplatedFlowFields, "agent_ipv6")
	if agentIP < 0 || agentIPv6 < 0 {
		t.Fatalf("expected picker fields to include agent_ip and agent_ipv6: %#v", generatedTemplatedFlowFields)
	}
	if agentIP > agentIPv6 {
		t.Fatalf("expected picker order agent_ip, agent_ipv6; got %#v", generatedTemplatedFlowFields)
	}
}

func TestGeneratedAggregateConfigPayloadPresetEnablesDataLinkFrame(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-output=ipfix:udp:127.0.0.1:4739", "-agg=payload", "-genconf"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected generated aggregator, got %d", len(cfg.Aggregators))
	}
	agg := cfg.Aggregators[0]
	if agg.Passthrough {
		t.Fatalf("expected payload preset alone to keep stateful aggregation")
	}
	if !aggregatorHasField(agg, "current", "frame_length") || !aggregatorHasField(agg, "current", "header_data") {
		t.Fatalf("expected payload preset to enable data-link fields, got %#v", agg.Fields)
	}
	fields := cfg.Encoder.TemplatedFlow.Data.Select
	if !slices.Contains(fields, "frame_length") || !slices.Contains(fields, "header_data") {
		t.Fatalf("expected payload preset to select data-link fields for IPFIX, got %#v", fields)
	}
	if aggregatorHasField(agg, "current", "nat_src_addr") || slices.Contains(fields, "nat_src_addr") {
		t.Fatalf("expected payload preset not to select NAT fields, agg=%#v fields=%#v", agg.Fields, fields)
	}
}

func TestGeneratedAggregateConfigLimitedPresetRemovesParsedPacketFields(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-output=ipfix:udp:127.0.0.1:4739", "-agg=payload,limited", "-genconf"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected generated aggregator, got %d", len(cfg.Aggregators))
	}
	agg := cfg.Aggregators[0]
	fields := cfg.Encoder.TemplatedFlow.Data.Select
	if !aggregatorHasField(agg, "current", "frame_length") || !aggregatorHasField(agg, "current", "header_data") {
		t.Fatalf("expected payload fields to remain, got %#v", agg.Fields)
	}
	for _, name := range []string{"src_addr", "dst_addr", "src_port", "dst_port", "src_mac", "dst_mac", "proto", "tcp_flags", "ether_type"} {
		if aggregatorHasFieldName(agg, name) || slices.Contains(fields, name) {
			t.Fatalf("expected limited preset to remove %s, agg=%#v fields=%#v", name, agg.Fields, fields)
		}
	}
	for _, name := range []string{"bytes", "packets", "input_if", "output_if", "flow_direction", "agent_ip", "agent_ipv6", "source_id"} {
		if !aggregatorHasFieldName(agg, name) || !slices.Contains(fields, name) {
			t.Fatalf("expected limited preset to preserve %s, agg=%#v fields=%#v", name, agg.Fields, fields)
		}
	}
}

func TestGeneratedAggregateConfigLimitedKeepsIPFieldsWithNATPreset(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-output=ipfix:udp:127.0.0.1:4739", "-agg=payload,limited,nat", "-genconf"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected generated aggregator, got %d", len(cfg.Aggregators))
	}
	agg := cfg.Aggregators[0]
	fields := cfg.Encoder.TemplatedFlow.Data.Select
	for _, name := range []string{"src_addr", "dst_addr", "nat_src_addr", "nat_dst_addr", "nat_src_port", "nat_dst_port"} {
		if !aggregatorHasFieldName(agg, name) || !slices.Contains(fields, name) {
			t.Fatalf("expected NAT limited preset to preserve %s, agg=%#v fields=%#v", name, agg.Fields, fields)
		}
	}
	for _, name := range []string{"src_port", "dst_port", "src_mac", "dst_mac", "proto", "tcp_flags", "ether_type"} {
		if aggregatorHasFieldName(agg, name) || slices.Contains(fields, name) {
			t.Fatalf("expected NAT limited preset to remove %s, agg=%#v fields=%#v", name, agg.Fields, fields)
		}
	}
}

func TestGeneratedAggregateConfigEncapPresetEnablesOuterFields(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-output=ipfix:udp:127.0.0.1:4739", "-agg=payload,limited,encap", "-genconf"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected generated aggregator, got %d", len(cfg.Aggregators))
	}
	if cfg.Processor.Builtin.PacketDecoder.DecodeBeyondL4 == nil || !*cfg.Processor.Builtin.PacketDecoder.DecodeBeyondL4 {
		t.Fatalf("expected encap preset to enable decode_beyond_l4, got %#v", cfg.Processor.Builtin.PacketDecoder.DecodeBeyondL4)
	}
	agg := cfg.Aggregators[0]
	fields := cfg.Encoder.TemplatedFlow.Data.Select
	for _, name := range []string{"outer_proto", "outer_src_port", "outer_src_addr", "outer_dst_port", "outer_dst_addr"} {
		if !aggregatorHasField(agg, "key", name) || !slices.Contains(fields, name) {
			t.Fatalf("expected encap preset to add key %s, agg=%#v fields=%#v", name, agg.Fields, fields)
		}
	}
	for _, name := range []string{"outer_proto_name", "encap_depth"} {
		if !aggregatorHasField(agg, "current", name) || !slices.Contains(fields, name) {
			t.Fatalf("expected encap preset to add current %s, agg=%#v fields=%#v", name, agg.Fields, fields)
		}
	}
	for _, name := range []string{"src_addr", "dst_addr", "src_port", "dst_port"} {
		if aggregatorHasFieldName(agg, name) || slices.Contains(fields, name) {
			t.Fatalf("expected limited encap preset to keep inner field %s removed, agg=%#v fields=%#v", name, agg.Fields, fields)
		}
	}
}

func TestGeneratedAggregateConfigNATPresetEnablesNATFields(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-output=ipfix:udp:127.0.0.1:4739", "-agg=nat", "-genconf"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected generated aggregator, got %d", len(cfg.Aggregators))
	}
	agg := cfg.Aggregators[0]
	fields := cfg.Encoder.TemplatedFlow.Data.Select
	if !aggregatorHasField(agg, "current", "nat_src_addr") || !slices.Contains(fields, "nat_src_addr") {
		t.Fatalf("expected nat preset to add and select NAT fields, agg=%#v fields=%#v", agg.Fields, fields)
	}
	if aggregatorHasField(agg, "current", "frame_length") || aggregatorHasField(agg, "current", "header_data") {
		t.Fatalf("expected nat preset not to enable payload fields, got %#v", agg.Fields)
	}
}

func TestGeneratedAggregateConfigMPLSPresetEnablesMPLSFields(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-output=ipfix:udp:127.0.0.1:4739", "-agg=mpls", "-genconf"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected generated aggregator, got %d", len(cfg.Aggregators))
	}
	if cfg.Processor.Builtin.AggregationHelpers.MPLSLabels != len(generatedTemplatedFlowMPLSFields) {
		t.Fatalf("expected mpls aggregation helpers to be enabled, got %d", cfg.Processor.Builtin.AggregationHelpers.MPLSLabels)
	}
	agg := cfg.Aggregators[0]
	fields := cfg.Encoder.TemplatedFlow.Data.Select
	if !aggregatorHasField(agg, "current", "mpls_label_stack_section_1") || !slices.Contains(fields, "mpls_label_stack_section_1") {
		t.Fatalf("expected mpls preset to add and select MPLS fields, agg=%#v fields=%#v", agg.Fields, fields)
	}
}

func TestGeneratedAggregateConfigSupportsPassthroughPreset(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-agg=passthrough", "-genconf"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected generated aggregator, got %d", len(cfg.Aggregators))
	}
	agg := cfg.Aggregators[0]
	if !agg.Passthrough {
		t.Fatalf("expected passthrough preset to use schema passthrough")
	}
	if len(agg.Sum) != 0 || aggregatorHasRole(agg, "sum") {
		t.Fatalf("expected passthrough preset to remove sum fields, got sum=%#v fields=%#v", agg.Sum, agg.Fields)
	}
	if agg.Window.IdleFlushAfter != 0 || agg.Periodic.Every != 0 {
		t.Fatalf("expected passthrough preset to remove export timers, got window=%#v periodic=%#v", agg.Window, agg.Periodic)
	}
}

func TestConfigModeAllowsAggregationPresets(t *testing.T) {
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
  type: ipfix

sink:
  type: udp
  address: "127.0.0.1:4739"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-config", cfgPath, "-agg=passthrough,payload"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if generated {
		t.Fatalf("expected explicit config mode")
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected one overlay aggregator, got %d", len(cfg.Aggregators))
	}
	agg := cfg.Aggregators[0]
	if !agg.Passthrough {
		t.Fatalf("expected passthrough preset to use schema passthrough")
	}
	if !aggregatorHasField(agg, "current", "frame_length") || !aggregatorHasField(agg, "current", "header_data") {
		t.Fatalf("expected payload fields, got %#v", agg.Fields)
	}
}

func TestConfigModeMPLSPresetAddsMPLSFields(t *testing.T) {
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

encoder:
  type: json

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := BindFlags(fs)
	if err := fs.Parse([]string{"-config", cfgPath, "-agg=mpls"}); err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	cfg, generated, err := LoadFromFlags(flags)
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if generated {
		t.Fatalf("expected explicit config mode")
	}
	if cfg.Processor.Builtin.AggregationHelpers.MPLSLabels != len(generatedTemplatedFlowMPLSFields) {
		t.Fatalf("expected mpls aggregation helpers to be enabled, got %d", cfg.Processor.Builtin.AggregationHelpers.MPLSLabels)
	}
	if len(cfg.Aggregators) != 1 {
		t.Fatalf("expected one overlay aggregator, got %d", len(cfg.Aggregators))
	}
	if !aggregatorHasField(cfg.Aggregators[0], "current", "mpls_label_stack_section_1") {
		t.Fatalf("expected mpls preset to add aggregate MPLS fields, got %#v", cfg.Aggregators[0].Fields)
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
    emit_extended_records: false
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
	if cfg.Encoder.SFlow.EmitExtendedRecords == nil || *cfg.Encoder.SFlow.EmitExtendedRecords {
		t.Fatalf("expected sflow.emit_extended_records=false")
	}
	if cfg.Encoder.AllowTruncate == nil || !*cfg.Encoder.AllowTruncate {
		t.Fatalf("expected sflow allow_truncate to default true")
	}
	if cfg.Encoder.SFlow.MaxHeaderBytes != 128 {
		t.Fatalf("expected sflow.max_header_bytes default 128, got %d", cfg.Encoder.SFlow.MaxHeaderBytes)
	}
	if cfg.Encoder.Batch.IsEnabled() {
		t.Fatalf("expected YAML sflow config not to enable batching by default")
	}
}

func TestLoadSupportsAutoWorkers(t *testing.T) {
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "reflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
sources:
  - network: udp
    address: ":18081"
    type: json

processor:
  type: builtin
  workers: auto

encoder:
  type: ipfix
  workers: auto

sink:
  type: udp
  address: "127.0.0.1:4739"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Processor.Workers != AutoWorkers {
		t.Fatalf("expected processor workers auto, got %d", cfg.Processor.Workers)
	}
	if cfg.Encoder.Workers != AutoWorkers {
		t.Fatalf("expected encoder workers auto, got %d", cfg.Encoder.Workers)
	}
	if got := ResolveProcessorWorkers(cfg.Processor.Workers); got != runtime.NumCPU() {
		t.Fatalf("expected auto processor workers to resolve to CPU count, got %d", got)
	}
	if got := ResolveEncoderWorkers(cfg.Encoder.Workers, cfg.Encoder.Type); got != 1 {
		t.Fatalf("expected ordered auto encoder workers to resolve to 1, got %d", got)
	}
	if got := ResolveEncoderWorkers(2, cfg.Encoder.Type); got != 1 {
		t.Fatalf("expected ordered explicit encoder workers to resolve to 1, got %d", got)
	}
	if got := ResolveEncoderWorkers(AutoWorkers, "pcap"); got != 1 {
		t.Fatalf("expected pcap auto encoder workers to resolve to 1, got %d", got)
	}
	if got := ResolveEncoderWorkers(AutoWorkers, "json"); got != runtime.NumCPU() {
		t.Fatalf("expected unordered auto encoder workers to resolve to CPU count, got %d", got)
	}
}

func TestLoadSupportsSFlowTruncationOverrides(t *testing.T) {
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
  allow_truncate: false
  sflow:
    max_header_bytes: 256

sink:
  type: stdout
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Encoder.AllowTruncate == nil || *cfg.Encoder.AllowTruncate {
		t.Fatalf("expected explicit allow_truncate=false")
	}
	if cfg.Encoder.SFlow.MaxHeaderBytes != 256 {
		t.Fatalf("expected sflow.max_header_bytes=256, got %d", cfg.Encoder.SFlow.MaxHeaderBytes)
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

func TestHelperOptionsTextListsInputOutputAndAggregationExamples(t *testing.T) {
	text := HelperOptionsText()
	for _, want := range []string{
		"Input helper specs",
		"Output helper specs",
		"Aggregation helper specs",
		"-input udp::6343:flow",
		"-output json:stdout",
		"-agg passthrough",
		"-agg mpls",
		"-agg idle_flush_after_ms=<ms>,periodic_every_ms=<ms>",
		"encoders: json, protobuf, sflow, ipfix, netflowv9, netflowv5, pcap, pcapng",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected helper options to contain %q, got:\n%s", want, text)
		}
	}
}

func TestGeneratedSFlowOutputAllowsTruncate(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{
		Inputs:    []string{"ebpf:br-lan:bytes?snaplen=65535&sample_every=1"},
		Output:    "sflow:udp:127.0.0.1:6343",
		OutputSet: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if cfg.Encoder.Type != "sflow" {
		t.Fatalf("expected sflow encoder, got %q", cfg.Encoder.Type)
	}
	if cfg.Encoder.AllowTruncate == nil || !*cfg.Encoder.AllowTruncate {
		t.Fatalf("expected helper sflow output to enable allow_truncate")
	}
	if !cfg.Encoder.Batch.IsEnabled() {
		t.Fatalf("expected helper sflow output to enable batching by default")
	}
	if cfg.Encoder.Workers != AutoWorkers {
		t.Fatalf("expected helper sflow output to keep encoder workers auto, got %d", cfg.Encoder.Workers)
	}
}

func TestGeneratedIPFIXOutputEnablesBatching(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{
		Output:    "ipfix:udp:127.0.0.1:4739",
		OutputSet: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if !cfg.Encoder.Batch.IsEnabled() {
		t.Fatalf("expected helper ipfix output to enable batching by default")
	}
	if cfg.Encoder.Batch.MaxRecords != 32 {
		t.Fatalf("expected helper ipfix output to default batch max records to 32, got %d", cfg.Encoder.Batch.MaxRecords)
	}
	if cfg.Encoder.Workers != AutoWorkers {
		t.Fatalf("expected helper ipfix output to keep encoder workers auto, got %d", cfg.Encoder.Workers)
	}
}

func TestGeneratedNetFlowV9OutputEnablesBatching(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{
		Output:    "netflowv9:udp:127.0.0.1:2055",
		OutputSet: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if !cfg.Encoder.Batch.IsEnabled() {
		t.Fatalf("expected helper netflowv9 output to enable batching by default")
	}
	if cfg.Encoder.Batch.MaxRecords != 32 {
		t.Fatalf("expected helper netflowv9 output to default batch max records to 32, got %d", cfg.Encoder.Batch.MaxRecords)
	}
	if cfg.Encoder.Workers != AutoWorkers {
		t.Fatalf("expected helper netflowv9 output to keep encoder workers auto, got %d", cfg.Encoder.Workers)
	}
}

func TestGeneratedOutputParsesBatchParams(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{
		Output:    "ipfix:udp:127.0.0.1:4739?batch=true&batch_max_records=32&batch_max_bytes=4096&batch_flush_interval_ms=250",
		OutputSet: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if !cfg.Encoder.Batch.IsEnabled() {
		t.Fatalf("expected explicit batch=true")
	}
	if cfg.Encoder.Batch.MaxRecords != 32 || cfg.Encoder.Batch.MaxBytes != 4096 || cfg.Encoder.Batch.FlushInterval != 250 {
		t.Fatalf("unexpected batch params: %#v", cfg.Encoder.Batch)
	}

	sflowCfg, generated, err := LoadFromFlags(&FlagConfig{
		Output:    "sflow:udp:127.0.0.1:6343?batch=true&batch_max_records=32&batch_max_bytes=4096&batch_flush_interval_ms=250",
		OutputSet: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags sflow returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated sflow config")
	}
	if sflowCfg.Encoder.Batch.MaxBytes != 4096 || sflowCfg.Encoder.MaxDatagramBytes != 4096 {
		t.Fatalf("expected sflow batch/max datagram bytes 4096, got batch=%d max_datagram=%d", sflowCfg.Encoder.Batch.MaxBytes, sflowCfg.Encoder.MaxDatagramBytes)
	}

	netflowV9Cfg, generated, err := LoadFromFlags(&FlagConfig{
		Output:    "netflowv9:udp:127.0.0.1:2055?batch=true&batch_max_records=32&batch_max_bytes=4096&batch_flush_interval_ms=250",
		OutputSet: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags netflowv9 returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated netflowv9 config")
	}
	if netflowV9Cfg.Encoder.Batch.MaxRecords != 32 || netflowV9Cfg.Encoder.Batch.MaxBytes != 4096 || netflowV9Cfg.Encoder.Batch.FlushInterval != 250 {
		t.Fatalf("unexpected netflowv9 batch params: %#v", netflowV9Cfg.Encoder.Batch)
	}
	if netflowV9Cfg.Encoder.MaxDatagramBytes != 4096 {
		t.Fatalf("expected netflowv9 max datagram bytes 4096, got %d", netflowV9Cfg.Encoder.MaxDatagramBytes)
	}
}

func TestGeneratedOutputParsesBatchDisable(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{
		Output:    "sflow:udp:127.0.0.1:6343?batch=false",
		OutputSet: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if cfg.Encoder.Batch.IsEnabled() {
		t.Fatalf("expected explicit batch=false")
	}
}

func TestLoadDoesNotEnableIPFIXBatchingByDefault(t *testing.T) {
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
  type: ipfix

sink:
  type: udp
  address: "127.0.0.1:4739"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Encoder.Batch.IsEnabled() {
		t.Fatalf("expected YAML ipfix config not to enable batching by default")
	}
}

func TestGeneratedSFlowOutputParsesAllowTruncateParam(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{
		Output:    "sflow:udp:127.0.0.1:6343?allow_truncate=false&max_header_bytes=256",
		OutputSet: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config")
	}
	if cfg.Encoder.AllowTruncate == nil || *cfg.Encoder.AllowTruncate {
		t.Fatalf("expected explicit allow_truncate=false to override helper default")
	}
	if cfg.Encoder.SFlow.MaxHeaderBytes != 256 {
		t.Fatalf("expected explicit max_header_bytes=256, got %d", cfg.Encoder.SFlow.MaxHeaderBytes)
	}
}

func TestGeneratedOutputRejectsUnsupportedParams(t *testing.T) {
	_, _, err := LoadFromFlags(&FlagConfig{
		Output:    "json:stdout?allow_truncate=true",
		OutputSet: true,
	})
	if err == nil {
		t.Fatalf("expected json output to reject allow_truncate")
	}
	if !strings.Contains(err.Error(), "only supported for sflow") {
		t.Fatalf("expected sflow-only error, got %v", err)
	}
}

func TestGeneratedOutputRejectsUnsupportedBatchParams(t *testing.T) {
	_, _, err := LoadFromFlags(&FlagConfig{
		Output:    "json:stdout?batch=true",
		OutputSet: true,
	})
	if err == nil {
		t.Fatalf("expected json output to reject batch")
	}
	if !strings.Contains(err.Error(), "only supported for sflow, ipfix, and netflowv9") {
		t.Fatalf("expected sflow/ipfix/netflowv9-only error, got %v", err)
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
	if cfg.Sources[0].SnapLen != 65535 || cfg.Sources[0].SampleEvery != 1 {
		t.Fatalf("expected generated source defaults, got %#v", cfg.Sources[0])
	}
	if cfg.Processor.Workers != AutoWorkers {
		t.Fatalf("expected generated processor workers auto, got %d", cfg.Processor.Workers)
	}
	if cfg.Processor.Builtin.PacketDecoder.DecodeBeyondL4 == nil || *cfg.Processor.Builtin.PacketDecoder.DecodeBeyondL4 {
		t.Fatalf("expected generated packet decoder decode_beyond_l4=false")
	}
	if cfg.Processor.Builtin.PacketDecoder.Encapsulations.VXLAN.Enabled == nil || *cfg.Processor.Builtin.PacketDecoder.Encapsulations.VXLAN.Enabled {
		t.Fatalf("expected generated vxlan enabled=false")
	}
	if !slices.Contains(cfg.Processor.Builtin.PacketDecoder.Encapsulations.VXLAN.Ports, 4789) {
		t.Fatalf("expected generated vxlan default port 4789, got %#v", cfg.Processor.Builtin.PacketDecoder.Encapsulations.VXLAN.Ports)
	}
	if cfg.Encoder.TemplatedFlow.TemplateBaseID != 256 || cfg.Encoder.TemplatedFlow.OptionsTemplateBaseID != 1024 {
		t.Fatalf("expected generated templated flow base IDs, got %#v", cfg.Encoder.TemplatedFlow)
	}
	if cfg.Encoder.TemplatedFlow.TemplateRefresh != 60000 || cfg.Encoder.TemplatedFlow.OptionsRefresh != 30000 {
		t.Fatalf("expected generated templated flow refresh defaults, got %#v", cfg.Encoder.TemplatedFlow)
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	text := string(raw)
	if strings.Contains(text, ": null") {
		t.Fatalf("expected generated config to avoid null booleans, got:\n%s", text)
	}
	if strings.Contains(text, "ports: []") {
		t.Fatalf("expected generated config to materialize encapsulation ports, got:\n%s", text)
	}
}

func TestGeneratedEBPFConfigMaterializesFeatureDefaults(t *testing.T) {
	cfg, generated, err := LoadFromFlags(&FlagConfig{
		Inputs:  []string{"ebpf:br-lan:bytes"},
		GenConf: true,
	})
	if err != nil {
		t.Fatalf("LoadFromFlags returned error: %v", err)
	}
	if !generated {
		t.Fatalf("expected generated config mode")
	}
	if len(cfg.Sources) != 1 {
		t.Fatalf("expected one generated source, got %d", len(cfg.Sources))
	}
	src := cfg.Sources[0]
	if src.Network != "ebpf" || src.Interface != "br-lan" || src.Type != "bytes" {
		t.Fatalf("unexpected generated ebpf source: %#v", src)
	}
	if src.EBPF.SKBMetadata == nil || !*src.EBPF.SKBMetadata {
		t.Fatalf("expected generated ebpf skb_metadata=true, got %#v", src.EBPF.SKBMetadata)
	}
	if src.EBPF.Conntrack == nil || !*src.EBPF.Conntrack {
		t.Fatalf("expected generated ebpf conntrack=true, got %#v", src.EBPF.Conntrack)
	}
	if src.EBPF.Direction != "both" {
		t.Fatalf("expected generated ebpf direction both, got %q", src.EBPF.Direction)
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
	if cfg.Sources[0].EBPF.Direction != "both" {
		t.Fatalf("expected ebpf direction both, got %q", cfg.Sources[0].EBPF.Direction)
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

func TestParseInputSpecSupportsCaptureParams(t *testing.T) {
	src, err := parseInputSpec("pcap_live:en0:bytes?snaplen=262144&sample_every=10&sample_offset=3")
	if err != nil {
		t.Fatalf("parseInputSpec returned error: %v", err)
	}
	if src.Network != "pcap_live" || src.Interface != "en0" || src.Type != "bytes" || src.Address != "" {
		t.Fatalf("unexpected pcap_live source config: %#v", src)
	}
	if src.SnapLen != 262144 || src.SampleEvery != 10 || src.SampleOffset != 3 {
		t.Fatalf("unexpected capture params: snaplen=%d sample_every=%d sample_offset=%d", src.SnapLen, src.SampleEvery, src.SampleOffset)
	}
}

func TestParseInputSpecSupportsEBPFFeatureParams(t *testing.T) {
	src, err := parseInputSpec("ebpf:br-lan:bytes?skb_metadata=false&conntrack=false&conntrack_path=/tmp/nf_conntrack&direction=ingress")
	if err != nil {
		t.Fatalf("parseInputSpec returned error: %v", err)
	}
	if src.EBPF.SKBMetadata == nil || *src.EBPF.SKBMetadata {
		t.Fatalf("expected skb_metadata=false, got %#v", src.EBPF.SKBMetadata)
	}
	if src.EBPF.Conntrack == nil || *src.EBPF.Conntrack {
		t.Fatalf("expected conntrack=false, got %#v", src.EBPF.Conntrack)
	}
	if src.EBPF.ConntrackPath != "/tmp/nf_conntrack" {
		t.Fatalf("expected conntrack path, got %q", src.EBPF.ConntrackPath)
	}
	if src.EBPF.Direction != "ingress" {
		t.Fatalf("expected direction ingress, got %q", src.EBPF.Direction)
	}
}

func TestParseInputSpecRejectsInvalidEBPFDirection(t *testing.T) {
	if _, err := parseInputSpec("ebpf:br-lan:bytes?direction=sideways"); err == nil {
		t.Fatalf("expected invalid ebpf direction to fail")
	}
}

func TestParseInputSpecRejectsCaptureParamsOnSocketInputs(t *testing.T) {
	if _, err := parseInputSpec("udp::6343:flow?snaplen=128"); err == nil {
		t.Fatalf("expected capture params on udp input to fail")
	}
}

func aggregatorHasField(agg AggregatorConfig, role, name string) bool {
	for _, field := range agg.Fields {
		if field.Role == role && field.Name == name {
			return true
		}
	}
	return false
}

func aggregatorHasFieldName(agg AggregatorConfig, name string) bool {
	for _, field := range agg.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func aggregatorHasRole(agg AggregatorConfig, role string) bool {
	for _, field := range agg.Fields {
		if field.Role == role {
			return true
		}
	}
	return false
}
