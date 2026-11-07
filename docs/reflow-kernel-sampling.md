# AF_PACKET kernel sampling

Packet selection can run before AF_PACKET packets enter the capture ring:

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
currently supported only for `af_packet`, with `algorithm: random` and
`sample_offset: 0`; unsupported requests fail instead of silently falling back.

Kernel sampling intentionally does not advertise a sample pool: the AF_PACKET
backend sees selected packets rather than the total number of eligible packets.
AF_PACKET socket drops describe admitted samples lost to capture pressure, not
packets intentionally excluded by the filter.
