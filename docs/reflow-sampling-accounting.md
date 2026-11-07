# Sampling rates and capture health

The generated packet aggregator keys on `sampling_rate` as well as the flow tuple
and source ID. Its IPFIX/NetFlow data records include the rate directly. Thus two
observations of the same flow at different rates produce separate buckets, even
when they share an export window. A collector need not infer a packet's rate from
the latest source options, which can be delayed, reordered, or belong to another
interface.

Counters remain raw observations. For a time window, estimate traffic with:

```
estimated_bytes = sum(bytes * sampling_rate)
MB_per_second = estimated_bytes / window_seconds / 1000000
```

Do not multiply an entire window by its final sampling rate. Sampling estimates
are noisy for short or sparse flows, and missing flows cannot be counted exactly.
Capture delivery losses are additional unknown traffic, not intentional sampling;
weighting does not recover them.

Hand-written packet aggregators should include `key:sampling_rate`, and explicit
IPFIX/NetFlow data selection lists must include `sampling_rate`. Keep independent
observation points in separate buckets with `source_id` (and exporter identity
when collecting remotely). Capturing both WAN and LAN views counts observations
at both points rather than unique traffic.

`capture_drops_total` is a cumulative 64-bit capture delivery loss counter,
exported in generated source options as IPFIX element 433
(`ignoredLayer2FrameTotalCount`). It survives beyond 2^32 losses and is independent
of legacy sFlow `drops`. It excludes packets intentionally rejected by kernel
sampling. A restart resets the counter. AF_PACKET reports captured skb observations;
GRO/GSO can make these differ from physical wire frame counts.

The collector must retain `options_data` in a separate health stream if its normal
flow ingestion discards records without `packets`. This repository does not modify
external log forwarding or OpenObserve configuration.
