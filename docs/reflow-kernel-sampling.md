# Linux kernel packet sampling

Packet selection can run before AF_PACKET packets enter the capture ring or
before the XDP and eBPF socket backends copy packets to perf buffers:

```yaml
sources:
  - network: af_packet
    type: bytes
    capture:
      interface: eth2
      snaplen: 256
      sampling:
        location: kernel
        algorithm: random
        every: 16
```

`every: 16` independently selects approximately one in sixteen packets. A Linux
classic BPF socket filter uses the kernel's per-packet random value. Selection
affects only the capture socket, so rejected samples are still forwarded normally.
The driver supplies the selected packet's original wire length and sampling rate;
the generic capture adapter does not apply the divisor a second time. Sampled
packet/byte counters remain raw observations. Weighted traffic totals are estimates,
and small flows can be missed entirely.

Omitting `sampling` preserves the previous userspace `sample_every` and
`sample_offset` behavior: systematic selection in the eligible receive order.
The explicit equivalent is `location: userspace, algorithm: systematic`.
`sampling.every` takes precedence over legacy `sample_every`. Kernel sampling is
supported for `af_packet`, `xdp`, and `ebpf`, with `algorithm: random` and
`sample_offset: 0`; unsupported requests fail instead of silently falling back.

For XDP, change `network` to `xdp` and use `capture.direction: ingress`. For
socket eBPF capture use `network: ebpf`; its existing SKB and conntrack options
remain available. These backends require their existing kernel privileges.
The XDP program returns `XDP_PASS` for every packet, selected or not.

Kernel sampling intentionally does not advertise a sample pool: these backends
deliver selected packets rather than the total number of eligible packets.
AF_PACKET socket drops describe admitted samples lost to capture pressure.
XDP/eBPF kernel sampling counts failed perf-output calls in per-CPU maps. The
overlapping perf lost-record count is not added again. Intentional exclusions
are absent from all delivery-loss counters. Fixed and adaptive divisors use the
same in-record sampling metadata; see [adaptive sampling](reflow-adaptive-sampling.md).
