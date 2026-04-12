# ReFlow Options Data Notes

These notes capture the intended direction for exporter metadata and option-template handling.

## Goal

ReFlow should support two families of templated export records:

* `flow_data`
* `options_data`

The same way normal flow/data records can drive templated export, option records should also flow through the runtime and result in option templates plus option data records.

## Event Shape

ReFlow should distinguish traffic data from exporter metadata explicitly in the event model.

Suggested fields:

```go
type Event struct {
    ...
    RecordKind string         // flow_data, options_data, packet
    Scope      map[string]any // option-template scope fields
    Fields     map[string]any // record fields
}
```

Meaning:

* `RecordKind=flow_data` is the normal flow/data export path
* `RecordKind=options_data` is metadata meant for option templates
* `Scope` carries scope fields such as `agent_ip`, `source_id`, `if_index`
* `Fields` carries record values such as `sampling_rate`, `sample_pool`, `drops`, `if_name`

## Source Emission

Sources should be able to emit metadata periodically in addition to normal traffic events.

Examples:

* current sampling rate
* sample pool
* drops
* capture interface metadata
* exporter identity metadata

Example conceptual source output:

```yaml
record_kind: options_data
scope:
  agent_ip: 192.0.2.1
  source_id: 7
fields:
  sampling_rate: 1000
  sample_pool: 123456
  drops: 0
```

## Aggregation

Option-style metadata should go through a dedicated aggregation path, separate from normal flow-data aggregation.

Expected behavior:

* usually no flow conversation keying
* mostly `current` semantics
* optional `first` semantics for stable identity fields
* periodic emission
* no idle-expiry by default unless explicitly configured

This means ReFlow should eventually support:

* flow-data aggregation
* options-data aggregation

even if both are implemented by the same internal aggregation engine.

## Export Path

The encoder, not the sink, should interpret `RecordKind`.

Responsibilities:

* encoder maps `flow_data` to data templates + data records
* encoder maps `options_data` to option templates + option data records
* sink remains transport-only and just sends bytes

This keeps protocol logic in the encoder and avoids leaking template semantics into UDP/file/stdout sinks.

## Template Flow

Data templates and option templates should both be first-class outputs of the templated export path.

That means:

* flow-data events may require data template emission
* options-data events may require option template emission
* both template families should be managed in the same general runtime model

In other words, the same way data templates are flowing, option templates should also flow.

## Config Direction

Likely future config shape:

```yaml
sources:
  - network: udp
    address: ":18081"
    type: flow
    emit_options:
      enabled: true
      periodic:
        every_ms: 60000
      include:
        - sampling_rate
        - sample_pool
        - drops
        - interface

aggregators:
  - enabled: true
    stream: flow_data
    ...
  - enabled: true
    stream: options_data
    periodic:
      every_ms: 60000
    key_fields: [agent_ip, source_id]
    first: [agent_ip, source_id]
    current: [sampling_rate, sample_pool, drops]
    template_id: 300

encoder:
  type: ipfix
  tflow_data:
    ...
```

This is a direction note, not a final schema.
