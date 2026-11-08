{{/*
  IPv4-only, ClickHouse/Helm schema for GoFlow2.
  Render this file using the SAME Helm context as the supplied template.
  Intentionally retains Kafka topic, group, Protobuf schema, broker, and replica path convention.
  Executable schema objects: flows (Kafka), flows_raw, flows_raw_readable (ordinary VIEW),
  flows_1m (SummingMergeTree), flows_1m_view (MV), flows_raw_view (Kafka -> raw MV).
  Important: source protobuf has 16-byte IPs; only IPv4 flows from IPv4 exporters are ingested.
  Apply reset SQL BEFORE init when switching from the old schema; IF NOT EXISTS does not migrate tables.
*/}}
{{- $operation := default "init" .sqlOperation -}}

{{- if or (eq $operation "init") (eq $operation "preflight") }}
-- Kafka consumer offsets belong to this group; do not change topic/group inadvertently.
SELECT throwIf(
    countIf(
        engine != 'Kafka'
        OR position(create_table_query, 'kafka_group_name = \'{{ include "goflow2.fullname" . }}-clickhouse\'') = 0
        OR position(create_table_query, 'kafka_topic_list = \'{{ .Values.kafka.topic }}\'') = 0
    ) > 0,
    'Existing Kafka source differs in engine/topic/group; migration aborted'
)
FROM system.tables
WHERE database = 'default' AND name = 'flows';
{{- end }}

{{- if eq $operation "init" }}

-- 1) Destination: compact IPv4 NetFlow rows. 30-day TTL at UTC day boundaries.
--    IPv4 is a 4-byte ClickHouse type displayed as dotted decimal in SELECT.
--    Nanoseconds are retained because DateTime64(9) and DateTime64(3) both use 8 bytes.
CREATE TABLE IF NOT EXISTS default.flows_raw
(
    date Date CODEC(ZSTD(1)),
    time_inserted_ns DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(1)),
    time_received_ns DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(1)),
    time_flow_start_ns DateTime64(9, 'UTC') CODEC(Delta(8), ZSTD(1)),

    sequence_num UInt32 CODEC(ZSTD(1)),
    sampling_rate UInt64 CODEC(ZSTD(1)),
    sampler_address IPv4 CODEC(ZSTD(1)),

    src_addr IPv4 CODEC(ZSTD(1)),
    dst_addr IPv4 CODEC(ZSTD(1)),

    src_as UInt32 CODEC(ZSTD(1)),
    dst_as UInt32 CODEC(ZSTD(1)),

    etype UInt16 CODEC(ZSTD(1)),
    proto UInt8 CODEC(ZSTD(1)),

    src_port UInt16 CODEC(ZSTD(1)),
    dst_port UInt16 CODEC(ZSTD(1)),

    in_if UInt32 CODEC(ZSTD(1)),
    out_if UInt32 CODEC(ZSTD(1)),

    bytes UInt64 CODEC(ZSTD(1)),
    packets UInt64 CODEC(ZSTD(1))
)
ENGINE = {{ if gt (int .Values.clickhouse.replicaCount) 1 }}ReplicatedMergeTree('/goflow2/tables/{shard}/flows_raw', '{replica}'){{ else }}MergeTree(){{ end }}
PARTITION BY date
ORDER BY (sampler_address, time_received_ns)
TTL date + INTERVAL 30 DAY DELETE
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- 2) Compact 1-minute rollup. Fast for total bps and top dst_port per exporter/proto.
--    It DOES NOT include src/dst IPs, src_port or interfaces; filter those in raw.
--    Sum again when reading: SummingMergeTree may have not merged all parts yet.
CREATE TABLE IF NOT EXISTS default.flows_1m
(
    date Date CODEC(ZSTD(1)),
    minute DateTime('UTC') CODEC(Delta(4), ZSTD(1)),
    sampler_address IPv4 CODEC(ZSTD(1)),
    proto UInt8 CODEC(ZSTD(1)),
    dst_port UInt16 CODEC(ZSTD(1)),
    bytes UInt64 CODEC(ZSTD(1)),
    packets UInt64 CODEC(ZSTD(1)),
    flow_records UInt64 CODEC(ZSTD(1))
)
ENGINE = {{ if gt (int .Values.clickhouse.replicaCount) 1 }}ReplicatedSummingMergeTree('/goflow2/tables/{shard}/flows_1m', '{replica}'){{ else }}SummingMergeTree(){{ end }}
PARTITION BY date
ORDER BY (sampler_address, date, minute, proto, dst_port)
TTL date + INTERVAL 30 DAY DELETE
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- 3) Insertion trigger from flows_raw to minute rollup.
--    Create BEFORE Kafka -> flows_raw MV to avoid missing the first ingested batches.
CREATE MATERIALIZED VIEW IF NOT EXISTS default.flows_1m_view
TO default.flows_1m
AS SELECT
    date,
    toStartOfMinute(time_received_ns) AS minute,
    sampler_address,
    proto,
    dst_port,
    sum(bytes) AS bytes,
    sum(packets) AS packets,
    count() AS flow_records
FROM default.flows_raw
GROUP BY date, minute, sampler_address, proto, dst_port;


-- 4) Kafka input schema MUST match existing Protobuf payload; its binary IPs are
--    NOT persisted in raw storage. Keep the existing topic and consumer group.
CREATE TABLE IF NOT EXISTS default.flows
(
    time_received_ns UInt64,
    time_flow_start_ns UInt64,
    sequence_num UInt32,
    sampling_rate UInt64,
    sampler_address FixedString(16),
    src_addr FixedString(16),
    dst_addr FixedString(16),
    src_as UInt32,
    dst_as UInt32,
    etype UInt32,
    proto UInt32,
    src_port UInt32,
    dst_port UInt32,
    in_if UInt32,
    out_if UInt32,
    bytes UInt64,
    packets UInt64
)
ENGINE = Kafka()
SETTINGS
    kafka_broker_list = '{{ include "goflow2.fullname" . }}-kafka:9092',
    kafka_num_consumers = 1,
    kafka_topic_list = '{{ .Values.kafka.topic }}',
    kafka_group_name = '{{ include "goflow2.fullname" . }}-clickhouse',
    kafka_format = 'Protobuf',
    kafka_schema = 'flow.proto:FlowMessage';

-- 5) Begin Kafka consumption LAST, after all dependent objects are created.
--    Source IP format for IPv4: first 4 bytes = address, last 12 bytes = zero.
--    This filter deliberately discards IPv6 traffic and non-IPv4 exporters.
--    If IPv6 flows/exporters are required, change the schema BEFORE deployment.
CREATE MATERIALIZED VIEW IF NOT EXISTS default.flows_raw_view
TO default.flows_raw
AS SELECT
    toDate(fromUnixTimestamp64Nano(toInt64(flows.time_received_ns), 'UTC')) AS date,
    now64(9, 'UTC') AS time_inserted_ns,
    fromUnixTimestamp64Nano(toInt64(flows.time_received_ns), 'UTC') AS time_received_ns,
    fromUnixTimestamp64Nano(toInt64(flows.time_flow_start_ns), 'UTC') AS time_flow_start_ns,

    flows.sequence_num AS sequence_num,
    flows.sampling_rate AS sampling_rate,
    toIPv4(IPv4NumToString(reinterpretAsUInt32(reverse(substring(flows.sampler_address, 1, 4))))) AS sampler_address,
    toIPv4(IPv4NumToString(reinterpretAsUInt32(reverse(substring(flows.src_addr, 1, 4))))) AS src_addr,
    toIPv4(IPv4NumToString(reinterpretAsUInt32(reverse(substring(flows.dst_addr, 1, 4))))) AS dst_addr,
    flows.src_as AS src_as,
    flows.dst_as AS dst_as,
    toUInt16(flows.etype) AS etype,
    toUInt8(flows.proto) AS proto,
    toUInt16(flows.src_port) AS src_port,
    toUInt16(flows.dst_port) AS dst_port,
    flows.in_if AS in_if,
    flows.out_if AS out_if,
    flows.bytes AS bytes,
    flows.packets AS packets
FROM default.flows AS flows
WHERE flows.etype = 2048
  AND substring(flows.sampler_address, 5, 12) = unhex('000000000000000000000000');

{{- end }}
