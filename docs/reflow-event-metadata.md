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
  SFlowDec -->|"sampled header:\nprotocol, frame_length,\nheader_data"| Fields

  NF5Dec -->|"flow_type=netflowv5,\ntuple, counters, ifs, times"| Fields
  NF9Dec -->|"flow_type=netflowv9,\ntuple, counters, ifs,\nsampling_rate, times"| Fields
  IPFIXDec -->|"flow_type=ipfix,\ntuple, counters, ifs,\nsampling_rate, times"| Fields
  NF9Dec -->|"template/options events"| Control
  IPFIXDec -->|"template/options events"| Control

  BytesDec -->|"message_type=bytes"| Fields

  subgraph Processor
    Normalize["packet.NormalizeEvent"]
  end

  Fields --> Normalize
  SFlowMeta --> Normalize
  Normalize -->|"fills defaults:\nrecord_kind, frame_length,\nbytes, packets, times,\nprotocol"| Fields
  Normalize -->|"parses header_data"| Packet
  Normalize -->|"fills missing sflow.agent_ip\nfrom fields.agent_ip"| SFlowMeta

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
  Fields -->|"input_if, output_if,\nprotocol, frame_length,\nstripped, original_length,\nheader_data, counters"| SFlowEnc

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

IPFIX and NetFlow do not currently have dedicated top-level metadata structs.
Their protocol information is represented through canonical `fields`, control
events, and encoder state. For example, IPFIX output reads
`observation_domain_id`, `template_id`, `sampling_rate`, and flow tuple fields
from `fields`, while schema and source initialization control events drive
template and options output.

Canonical JSON output preserves the event envelope, including `source`,
`sflow`, `fields`, and `packet`. Vendor and GoFlow2-compatible JSON flavors
flatten or select fields and do not preserve the full metadata envelope.

