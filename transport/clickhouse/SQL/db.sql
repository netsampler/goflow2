CREATE DATABASE IF NOT EXISTS flows ON CLUSTER ...;

CREATE TABLE IF NOT EXISTS flows.flows_local ON CLUSTER ...
(
    `ts` DateTime MATERIALIZED now(),
    `date` Date MATERIALIZED toDate(ts),
    `type` UInt32,
    `time_received_ns` UInt64,
    `sequence_num` UInt32,
    `sampling_rate` UInt64,
    `sampler_address` String,
    `time_flow_start_ns` UInt64,
    `time_flow_end_ns` UInt64,
    `bytes` UInt64,
    `packets` UInt64,
    `src_addr` String,
    `dst_addr` String,
    `etype` UInt32,
    `proto` UInt32,
    `src_port` UInt32,
    `dst_port` UInt32,
    `in_if` UInt32,
    `out_if` UInt32,
    `src_mac` UInt64,
    `dst_mac` UInt64,
    `src_vlan` UInt32,
    `dst_vlan` UInt32,
    `vlan_id` UInt32,
    `ip_tos` UInt32,
    `forwarding_status` UInt32,
    `ip_ttl` UInt32,
    `ip_flags` UInt32,
    `tcp_flags` UInt32,
    `icmp_type` UInt32,
    `icmp_code` UInt32,
    `ipv6_flow_label` UInt32,
    `fragment_id` UInt32,
    `fragment_offset` UInt32,
    `src_as` UInt32,
    `dst_as` UInt32,
    `next_hop` String,
    `next_hop_as` UInt32,
    `src_net` UInt32,
    `dst_net` UInt32,
    `bgp_next_hop` String,
    `bgp_communities` Array(UInt32),
    `as_path` Array(UInt32),
    `mpls_ttl` Array(UInt32),
    `mpls_label` Array(UInt32),
    `mpls_ip` Array(String),
    `observation_domain_id` UInt32,
    `observation_point_id` UInt32,
    `flow_direction` UInt32 DEFAULT 255,
    `rbytes` UInt64 MATERIALIZED multiIf(sampling_rate > 0, bytes * sampling_rate, bytes),
    `rpackets` UInt64 MATERIALIZED multiIf(sampling_rate > 0, packets * sampling_rate, packets),
    `sampler_name` String
)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/{database}/{table}', '{replica}')
PARTITION BY toStartOfHour(ts)
ORDER BY ts
TTL ts + toIntervalHour(1)
SETTINGS index_granularity = 8192;

CREATE TABLE IF NOT EXISTS flows.flows ON CLUSTER ... AS flows.flows_local 
ENGINE = Distributed(...,
 'flows',
 'flows_local',
 rand());
