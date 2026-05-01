# ReFlow Event Metadata

This page shows which ReFlow stages populate event metadata and which encoders
consume each field. The runtime event shape is defined in
`internal/reflow/event/event.go`.

```mermaid
flowchart LR
  subgraph Sources
    Socket["socket source\nudp/unixgram"]
    Pcap["pcap_live source"]
  end

  subgraph EventEnvelope["event.Event envelope"]
    SourceMeta["source\nnetwork, address, remote,\ntype, capture_interface"]
    SFlowMeta["sflow\nagent_ip, sub_agent_id,\nsequence_number, uptime,\nsource_id, sampling_rate,\nsample_pool, drops"]
    Fields["fields map\ncanonical flow/packet fields"]
    Packet["packet\nparsed packet model"]
    Control["control/source_init/schema"]
  end

  Socket -->|"sets source metadata\nputs datagram in Payload"| SourceMeta
  Pcap -->|"sets source metadata"| SourceMeta
  Pcap -->|"agent_ip, sampling_rate,\nsample_pool, drops"| SFlowMeta
  Pcap -->|"agent_ip, sampling_rate,\nsample_pool, drops,\ncapture_length, wire_length"| Fields
  Pcap -->|"source_init control event:\nagent_ip, source_id,\nsampling_rate, input_if, output_if"| Control

  subgraph Decoders
    SFlowDec["sFlow decoder"]
    NF5Dec["NetFlow v5 decoder"]
    NF9Dec["NetFlow v9 decoder"]
    IPFIXDec["IPFIX decoder"]
    BytesDec["bytes decoder"]
  end

  Socket --> Decoders

  SFlowDec -->|"packet/sample metadata"| SFlowMeta
  SFlowDec -->|"flow_type=sflow, agent_ip,\nsource_id, sampling_rate,\nsample_pool, drops,\ninput_if, output_if"| Fields
  SFlowDec -->|"sampled header:\nrecord_kind=packet,\nprotocol, frame_length,\nheader_data"| Fields
  SFlowDec -->|"interface counters:\nrecord_kind=interface_counter,\ncounter_type=sflow, if_* fields"| Fields

  NF5Dec -->|"flow_type=netflowv5,\ntuple, counters, ifs, times"| Fields
  NF9Dec -->|"flow_type=netflowv9,\ntuple, counters, ifs,\nsampling_rate, times"| Fields
  IPFIXDec -->|"flow_type=ipfix,\ntuple, counters, ifs,\nsampling_rate, times"| Fields
  NF9Dec -->|"record_kind=template,\noptions_template, options_data"| Fields
  IPFIXDec -->|"record_kind=template,\noptions_template, options_data"| Fields
  NF9Dec -->|"template/options events"| Control
  IPFIXDec -->|"template/options events"| Control

  BytesDec -->|"record_kind=packet"| Fields

  subgraph Processor
    Normalize["packet.NormalizeEvent"]
  end

  Fields --> Normalize
  SFlowMeta --> Normalize
  Normalize -->|"fills defaults:\nrecord_kind, frame_length,\nbytes, packets, times,\nprotocol"| Fields
  Normalize -->|"packet-derived fields:\nsrc_addr/dst_addr,\nouter_* aliases"| Fields
  Normalize -->|"parses header_data"| Packet

  subgraph Encoders
    JSONEnc["JSON encoder"]
    ProtoEnc["protobuf encoder"]
    SFlowEnc["sFlow encoder"]
    IPFIXEnc["IPFIX encoder"]
    NF9Enc["NetFlow v9 encoder"]
    NF5Enc["NetFlow v5 encoder"]
  end

  SourceMeta --> JSONEnc
  SFlowMeta --> JSONEnc
  Fields --> JSONEnc
  Packet --> JSONEnc

  SFlowMeta -->|"prefers metadata for\nsequence_num, sampling_rate,\nsampler_address"| ProtoEnc
  Fields -->|"tuple, counters, ifs,\nobs domain/point"| ProtoEnc

  SFlowMeta -->|"agent_ip, sub_agent_id,\nsequence_number, uptime,\nsource_id, sampling_rate,\nsample_pool, drops"| SFlowEnc
  Fields -->|"record_kind=packet:\ninput_if, output_if,\nprotocol, frame_length,\nstripped, original_length,\nheader_data"| SFlowEnc
  Fields -->|"record_kind=interface_counter:\nif_* counter fields"| SFlowEnc

  Fields -->|"template_id,\nobservation_domain_id,\nflow fields selected by catalog"| IPFIXEnc
  Control -->|"schema/source_init creates\ntemplates/options records"| IPFIXEnc

  Fields -->|"source_id,\ntemplate_id,\nflow fields selected by catalog"| NF9Enc
  Control -->|"schema/source_init creates\ntemplates/options records"| NF9Enc

  Fields -->|"IPv4 tuple, bytes,\npackets, ifs, times,\nsampling_rate"| NF5Enc
```

## Notes

ReFlow has a dedicated top-level `sflow` metadata block. It is populated by
sFlow decoding and by `pcap_live` packet capture, and sFlow/protobuf encoders
prefer it for sFlow-specific values when present.

Packet normalization is not responsible for hydrating sFlow metadata. It
normalizes packet/header fields such as `record_kind`, `frame_length`,
`header_data`, `bytes`, `packets`, and packet-derived tuple fields. The sFlow
encoder resolves sFlow-specific values from encoder config, `event.SFlow`, and
generic `fields` in that order, depending on the value.

When sFlow output needs sampled-header bytes and the event only has tuple or
packet-model data, the sFlow encoder builds a synthetic sampled header locally.
That keeps pseudo-packet construction tied to the only output format that
requires it. For encapsulated flows, `packet.layers` is the authoritative layer
model.

Packet-derived tuple fields are available as convenience aliases. The flat
fields (`src_addr`, `dst_addr`, `proto`, `src_port`, `dst_port`) describe the
innermost flow tuple, while `outer_*` fields remain aliases for the first
encapsulating IP tuple. Aggregation configs can use those aliases when they need
stable keys across packets whose full layer stacks have different non-IP layers.
Parsed VLAN tags also populate Dot1Q aliases: `dot1q_vlan_id`,
`dot1q_priority`, and `dot1q_dei` for the outer tag, plus
`dot1q_customer_vlan_id`, `dot1q_customer_priority`, and
`dot1q_customer_dei` for the second tag when present. Parsed VXLAN packets
populate `vxlan_vni` and the standard IPFIX `layer2_segment_id` helper.

Aggregation helper fields are opt-in under
`processor.builtin.aggregation_helpers`. `mpls_labels: N` emits
`mpls_label_1..N` numeric labels for aggregation keys and
`mpls_label_stack_section_1..N` 3-byte IPFIX MPLS label stack section values
for export. `ip_layers: N` emits `ip_1_*`, `ip_2_*`, ... aliases, ordered from
outermost IP tuple to innermost parsed IP tuple, with `src_addr`, `dst_addr`,
`proto`, `proto_name`, `src_port`, and `dst_port` for each layer. The default
IPFIX catalog does not map `outer_*` or `ip_N_*` helper fields to the standard
source/destination address elements, because IPFIX does not define generic
outer/encapsulated address elements. Use them directly for aggregation keys or
JSON output, or add explicit enterprise/catalog mappings for an IPFIX collector
that understands those semantics.

```yaml
processor:
  type: builtin
  builtin:
    aggregation_helpers:
      mpls_labels: 3
      ip_layers: 2
```

Packet decoding policy lives under `processor.builtin.packet_decoder`.
`decode_beyond_l4` controls whether the parser may continue past TCP/UDP/ICMP
or the first encapsulation header. Encapsulation-specific settings live under
`packet_decoder.encapsulations`, where operators can enable/disable GRE,
IP-in-IP, VXLAN, Geneve, L2TP, GTP-U, and PPPoE handling. UDP-based
encapsulations can use non-standard port lists. MPLS labels and IPv6 extension
headers are always parsed when present; `decode_beyond_l4` controls whether the
parser continues into encapsulated payloads. GRE uses IP protocol `47`; IP-in-IP
covers both IPv4 and IPv6 inner packets, using IP protocols `4` and `41`.

## Aggregator Field Entries

Every item in `aggregators` is active. To disable an aggregator, remove or
comment out that list item.

Aggregator `fields` entries describe aggregation policy and schema order only.
They do not choose IPFIX or NetFlow information element IDs. Protocol mapping
stays in `encoder.tflow_data`, including enterprise/PEN, field ID, type, and
length overrides.

Compact entries use colon-separated parts:

```text
role:name
static:name:value
```

`role` can be `key`, `sum`, `first`, `current`, `min`, `max`, or `static`.
`current` includes the field in pass-through schemas and keeps the latest value
when stateful aggregation is active. IPFIX and NetFlow v9 templates for
`src_addr` and `dst_addr` automatically use IPv4 or IPv6 information elements
based on the event address family.

```yaml
aggregators:
  - stream: flow_data
    fields:
      - key:src_addr
      - key:dst_addr
      - current:tenant_id
      - sum:bytes
      - sum:packets
      - current:end_time_unix
      - static:exporter_name:edge-a
```

If no stateful rollup is required, the aggregator is schema pass-through. It
emits a schema event when `fields` are configured and forwards matching events
to the encoder immediately. This is the mode to use when aggregation is acting
as a filter in order to reuse a template:

```yaml
aggregators:
  - stream: flow_data
    match:
      packet.has_layer.mpls: "true"
    fields:
      - key:src_addr
      - key:dst_addr
      - key:mpls_label
      - current:bytes
      - static:exporter_name:edge-a
```

Legacy `key_fields`, `sum`, `first`, `current`, and `static_fields` lists still
load, but they no longer receive hidden default aggregation fields. Omitting
`sum` means the sum list is empty. An aggregator with only `key`, `current`, and
`static` fields and no export trigger forwards matching events immediately.

When a custom field needs IPFIX enterprise mapping, configure it under the
encoder catalog or overrides instead:

```yaml
encoder:
  type: ipfix
  tflow_data:
    overrides:
      tenant_id:
        name: tenantId
        id: 12345
        pen: 32473
        enterprise_scoped: true
        length: 4
        type: unsigned32
```

## Source Framing

ReFlow distinguishes datagram input from stream input with `sources[].network`,
not with `sources[].type`.

`network: udp` and `network: unixgram` are packet-oriented. Each socket read
becomes one event, and for `type: flow` or `type: bytes` the complete datagram
is placed in `event.Payload`. For `type: json`, the datagram is parsed as one
JSON message.

`network: stream` is byte-stream oriented. `address: "-"` means stdin. Stream
sources do not preserve datagram boundaries; their `type` selects the framing
inside the stream:

| stream `type` | Framing |
| --- | --- |
| `json` | NDJSON, one JSON object per line |
| `pcap` | classic pcap stream records |
| `pcapng` | pcapng stream records |

Because stdin is only available through `network: stream`, stdin is never a
datagram source by itself. It can still carry packets or flow events when the
input format supplies its own framing, such as pcap, pcapng, or NDJSON.

`record_kind` is the generic record-shape marker. `packet` means the event
carries packet/header bytes in `header_data`; `interface_counter` means the
event carries interface counters; `template`, `options_template`, and
`options_data` represent NetFlow v9/IPFIX template state.

The low-level sFlow decoder understands expanded flow/counter samples and
several sFlow extended records. ReFlow currently projects sampled headers and
interface counters into canonical fields. Other sFlow extended records can be
decoded by the protocol package, but are not yet mapped into ReFlow fields.

IPFIX and NetFlow do not currently have dedicated top-level metadata structs.
Their protocol information is represented through canonical `fields`, control
events, and encoder state. For example, IPFIX output reads
`observation_domain_id`, `template_id`, `sampling_rate`, and flow tuple fields
from `fields`, while schema and source initialization control events drive
template and options output.

Canonical JSON output preserves the event envelope, including `source`,
`sflow`, `fields`, and `packet`. Vendor and GoFlow2-compatible JSON flavors
flatten or select fields and do not preserve the full metadata envelope.
