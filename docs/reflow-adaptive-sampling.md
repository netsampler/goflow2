# Adaptive Linux capture sampling

The `af_packet`, `xdp`, and `ebpf` kernel samplers can optionally adapt their
divisors when capture delivery loses packets. Existing configurations keep
their fixed sampling behavior.

```yaml
sources:
  - network: af_packet
    type: bytes
    source_id: 0
    capture:
      interface: eth2
      snaplen: 256
      direction: both
      sampling:
        location: kernel
        algorithm: random
        every: 16
        adaptive:
          enabled: true
          min_every: 1
          max_every: 256
          interval_ms: 1000
          recovery_after_ms: 30000
          increase_factor: 4
          decrease_factor: 2
          drop_threshold: 0
```

The statistics monitor runs independently of packet processing. For AF_PACKET it
reads the socket's resetting `PACKET_STATISTICS` counter once per interval;
XDP/eBPF use cumulative per-CPU counters of selected copies and failed
perf-output calls, independent of the blocked packet reader. A delivery drop
count greater than `drop_threshold` multiplies the divisor by `increase_factor`,
capped at `max_every`. Continuous traffic without delivery loss for
`recovery_after_ms` divides it by `decrease_factor`, bounded by `min_every`.
An interval without selected packet activity resets the recovery timer. Each
source has its own controller; an eBPF `any` source shares one divisor across
its interfaces.
Intentional rejection by the sampling filter does not count as capture loss.
There is no CPU-load or NIC-error heuristic in this controller.
Power-of-two divisors, bounds, and adjustment factors give exactly representable
selection probabilities with the 32-bit random filter. Other divisors approximate
1/N; very large non-power-of-two divisors can have material rounding error. The
defaults use powers of two. Keep bounds modest (for example, 1 through 256).

AF_PACKET rate changes use separate socket epochs. Reflow prepares a replacement
ring while the old socket still captures, stops admissions to the old socket,
synchronizes its kernel hook, and activates the replacement **before** draining
the old ring. Queued old packets retain their old divisor; the replacement
buffers new packets with the new divisor while the old backlog drains. A slow
consumer can still overflow the new ring, but draining no longer deliberately
pauses capture. Two configured rings coexist temporarily during a handoff, so
allow twice the configured AF_PACKET ring memory for each transitioning source.
The host continues forwarding throughout.

Every transition logs `capture_pause_ns`, cumulative `capture_pause_total_ns`,
and `sampling_transitions`. Device RX/TX byte counters bracket the handoff:
`capture_pause_rx_bytes` and `capture_pause_tx_bytes` report their deltas when
`capture_pause_counters_available=true`. These reads are not atomic with packet
admission or each other; `capture_pause_counter_interval_ns` records the wider
measurement interval. They measure interface traffic across the handoff, rather
than exact lost captured bytes. Missing/reset counters are marked unavailable,
not silently reported as zero. Measurements use Linux sysfs and add no polling
to the packet path.

Subsequent raw JSON packet events include `capture_sampling_pause_total_ns`,
`capture_sampling_transitions`, `capture_sampling_pause_rx_bytes_total`, and
`capture_sampling_pause_tx_bytes_total`. The boolean
`capture_sampling_pause_counters_complete` indicates whether all transitions
had usable counter snapshots. These are local diagnostics with no invented
IPFIX information elements. Custom aggregators must explicitly retain them to
include them in JSON rollups. Sampling weights cannot reconstruct traffic missed
inside a pause or lost to capture delivery pressure.

An AF_PACKET epoch logs `capture sampling epoch dropped packets` when it closes,
with that socket's actual `sample_every`, its `dropped` observation count and
`total_dropped` across epochs. Generic `capture packets dropped` refresh warnings
summarize the cumulative counter without claiming a single rate for losses that
may span multiple epochs. Neither counter measures dropped bytes: the loss must
not be converted using the current rate after an adaptation.

XDP and socket eBPF use the same configuration block. Set `network: xdp` and
`capture.direction: ingress`, or `network: ebpf` with the desired direction.
Their divisor lives in a BPF map and is read once before selecting each packet.
The program stamps that exact divisor into every copied record, so already
queued records retain their original rates, even when CPU buffers are drained
out of order. Updates do not detach the program, reopen the socket, or deliberately
pause capture. The XDP program continues returning `XDP_PASS` on every path.
Rate changes are logged with the network, interface, previous/new divisors, and
cumulative delivery loss. No new switch or firewall configuration is performed.

Flow counters remain raw selected observations. Preserve `sampling_rate` per
record and calculate estimates as `sum(bytes * sampling_rate) / seconds`.
Generated packet aggregation includes `key:sampling_rate`; hand-written
stateful packet aggregation must also keep it as a key. Explicit IPFIX/NetFlow v9
field selections must contain `sampling_rate`. Configurations that discard this
information are rejected when adaptation is enabled. IPFIX/NetFlow v9
passthrough schemas must also include `sampling_rate` (a `current` field suffices
because no counters are combined). NetFlow v5 and packet-file
outputs do not support this adaptive mode. Fixed sampling remains available.
For JSON, use the canonical or goflow2v2 flavor and retain `sampling_rate`.

The controller only reacts to its capture backend's delivery losses. It cannot detect
traffic that bypasses the observation point through forwarding offload, nor
repair losses that occurred before the rate was increased. Interface counters
remain useful as an independent check on traffic volume.
