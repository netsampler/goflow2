# Conntrack input and shared NAT enrichment

The native Linux platform build supports an independent `conntrack` input. It
owns a local Netlink subscription and a bounded cache which other packet inputs
can use after tuple decoding. The input performs an initial dump before ReFlow
starts processing packets, consumes NEW/UPDATE/DESTROY notifications, and resyncs
periodically. Connections created before subscription can lack kernel event
extensions when `nf_conntrack_events=2`; periodic dumps reconcile these entries
even if no destroy event is delivered. It requires `CAP_NET_ADMIN` in ReFlow's network namespace and
kernel conntrack/ctnetlink support. No firewall rules are installed or changed.

```yaml
sources:
  - network: conntrack
    conntrack:
      name: router
      zone: 0
      max_entries: 65536
      resync_interval_ms: 30000
      receive_buffer: 4194304
      emit_events: false
  - network: af_packet
    type: bytes
    source_id: 0
    capture:
      interface: eth2
      snaplen: 256
      direction: both
processor:
  type: builtin
  builtin:
    conntrack:
      input: router
      tuple_view: pre_nat
```

This is an excerpt to merge with the existing aggregator, encoder and sink
configuration. Keep NAT fields in aggregation and export, for example with the
existing `-agg nat` preset. The cache works independently of packet sampling;
lookup is performed only for packets reaching the processor. No per-packet
Netlink requests or `/proc` table scans are made.

The first version supports IPv4 TCP and UDP, including translated ports. A cache
is scoped to one configured zone in the process's network namespace. The
processor uses local capture networks by default. Set `networks: [af_packet]` to
limit enrichment, or explicitly include another input network whose tuples are
known to refer to this same local connection table. Avoid applying this cache to
remote exporters with overlapping private address spaces.

`tuple_view: observed` (default) preserves captured addresses and ports.
`tuple_view: pre_nat` normalizes `src_addr`, `dst_addr`, `src_port`, and `dst_port`
to the tuple before NAT **in that packet's direction**, and preserves captured
values in `observed_*` fields. `nat_src_addr`, `nat_dst_addr`, `nat_src_port`, and
`nat_dst_port` describe the post-NAT tuple in the same direction, matching the
standard IPFIX postNAT/postNAPT fields. Unchanged endpoints need no NAT field.

For an outgoing SNAT connection, pre-NAT is private-client -> remote-server and
post-NAT is public-router -> remote-server. For its return traffic, pre-NAT is
remote-server -> public-router and post-NAT is remote-server -> private-client.
Thus pre-NAT does not always mean a private destination on return traffic.
`conntrack_original_src_addr`, `conntrack_original_src_port`, and corresponding
`dst` fields identify the original connection independently of packet direction;
`conntrack_direction` distinguishes original and reply traffic.

`conntrack_match: false` preserves the observed tuple. Mappings can be absent for
untracked connections, late packets after destruction, incomplete subscription
history, expired snapshots, or cache capacity pressure. Nothing is inferred from
public IP addresses alone. Upstream ISP CGNAT mappings are not visible. Mapping
does not require `nf_conntrack_acct`; this source does not replace packet/byte
counters with conntrack accounting.

Periodic `conntrack_health` control events expose cache entries, hits, misses,
and capacity evictions. They need an output that retains controls (such as JSON)
to be visible downstream. `emit_events: true` also emits lifecycle records of
kind `conntrack` on the ordinary pipeline; these have no packet/byte totals and
should be routed independently of packet flow aggregation. Netlink receive loss
invalidates the cache and triggers a fresh dump; a failed resync stops the input
rather than continuing with uncertain mappings.

The older eBPF-only `/proc` enrichment remains available for compatibility. When
a shared input is bound, the shared lookup supplies NAT mapping for matching
packet networks instead of the legacy derivation. Set eBPF `conntrack: false`
when using the shared input to avoid the old source doing redundant table scans.
