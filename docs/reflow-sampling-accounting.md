# Sampling rates and capture health

The generated packet aggregator keys on `sampling_rate` as well as the flow
tuple and source ID. Observations of the same flow at different rates go into
separate buckets, even when they share an export window.

Adaptive IPFIX export defaults to `sampling_mode: selector`. Each flow record
references a `selectorId` (IE 302). Options records in the same IPFIX message
describe the selectors it uses with `selectorAlgorithm` (IE 304, uniform
probabilistic sampling) and `samplingProbability` (IE 311). A change from 1 in 64
to 1 in 256 produces two different selector IDs. The collector looks up the
probability for each record and weights its counters by the reciprocal. The
mapping travels with the records it describes, including over UDP. Selector
Options reserve IPFIX template ID 65535.

For collectors that expect the deprecated `samplingInterval` (IE 34), set
`encoder.templated_flow.sampling_mode: legacy` or use
`-output 'ipfix:udp:127.0.0.1:4739?sampling_mode=legacy'`. This exports the
divisor directly on each record. NetFlow v9 and non-adaptive IPFIX use that
encoding by default. Non-adaptive IPFIX can opt into `sampling_mode: selector`
for uniform random packet sampling. Use `legacy` for systematic sampling or
when imported sampling divisors do not describe uniform random selection.

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
