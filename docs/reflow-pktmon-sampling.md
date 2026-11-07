# Packet Monitor callback sampling

The Windows platform profile supports fixed or adaptive random selection inside
Reflow's Packet Monitor callback. Opt in with `location: callback`:

```yaml
sources:
  - network: pktmon
    type: bytes
    capture:
      interface: Ethernet
      snaplen: 256
      direction: both
      sampling:
        location: callback
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

Merge this source into an existing pipeline. Disabling `adaptive.enabled` keeps
the fixed random divisor. Omitting the sampling block keeps the existing
userspace systematic `sample_every`/`sample_offset` behavior. Callback selection
requires `algorithm: random` and `sample_offset: 0`.

Packet Monitor has already delivered the observation when this selection runs.
Reflow checks direction and interface eligibility, reads the divisor once, and
makes a random decision before copying packet bytes or allocating metadata.
Rejected observations do not enter the queue and are not counted as delivery
losses. Selected frames own their buffers and carry that exact divisor through
the queue; a later rate change cannot relabel a queued packet. The shared
capture adapter does not sample it a second time.

An independent monitor reuses the Linux samplers' overload/recovery policy. An
interval with delivery losses greater than `drop_threshold` raises the divisor
by `increase_factor`, up to `max_every`. Continuous selected activity without
loss for `recovery_after_ms` lowers it by `decrease_factor`, down to `min_every`.
An interval without selected activity resets recovery. Updates do not restart
the Packet Monitor session. Each Reflow source has its own controller.

Feedback includes Packet Monitor's missed-read/missed-write reports and Reflow's
bounded queue losses. These remain separately visible in
`pktmon_missed_reads`, `pktmon_missed_writes`, and `capture_queue_drops` on raw
canonical JSON events. They contribute to cumulative `capture_drops_total`;
intentional random exclusions and a packet's firewall drop reason do not.
The native API's upstream sample pool is unknown and remains zero. Local
sampling is identified as `sampling_location: callback` and
`callback_sample_every`, rather than reported as upstream sampling.

Keep `sampling_rate` in each exported data record and as a stateful aggregation
key. Generated packet aggregation and IPFIX/NetFlow v9 data schemas do this.
Custom configurations that discard the rate are rejected when adaptation is
enabled. JSON canonical/goflow2v2 and packet-sample sFlow outputs retain rates;
the existing adaptive restrictions on NetFlow v5, packet-file output and JSON
vendor flavor still apply. Raw packet/byte counters are observations; estimates
use `sum(bytes * sampling_rate)`, applying the weight once. IPFIX source options
retain the separate 64-bit capture-loss total.

This mode reduces copying and downstream work. Every matching observation still
travels through Packet Monitor's driver, stream ring, and callback dispatch.
It cannot guarantee relief from upstream saturation or recover those losses.
The streaming ABI also does not supply an original wire length; this feature
leaves that value unknown. Native Windows testing is required to establish
performance on a particular machine.

Microsoft documents the stream configuration and loss reports in
[PacketMonitorCreateRealtimeStream](https://learn.microsoft.com/en-us/windows/win32/pktmon/packetmonitor/nf-packetmonitor-packetmonitorcreaterealtimestream).
Its [capture constraints](https://learn.microsoft.com/en-us/windows/win32/pktmon/packetmonitor/nf-packetmonitor-packetmonitoraddcaptureconstraint)
filter packet headers; they provide no documented random sampling divisor.
