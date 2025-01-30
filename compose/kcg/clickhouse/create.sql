CREATE TABLE IF NOT EXISTS flows
(
    type Int32,
    time_received_ns UInt64,
    sequence_num UInt32,
    sampling_rate UInt64,

    sampler_address FixedString(16),

    time_flow_start_ns UInt64,
    time_flow_end_ns UInt64,

    bytes UInt64,
    packets UInt64,

    src_addr FixedString(16),
    dst_addr FixedString(16),

    src_as UInt32,
    dst_as UInt32,

    etype UInt32,

    proto UInt32,

    src_port UInt32,
    dst_port UInt32,

    forwarding_status UInt32,
    tcp_flags UInt32,
    icmp_type UInt32,
    icmp_code UInt32,

    fragment_id UInt32,
    fragment_offset UInt32
)
ENGINE = Kafka()
SETTINGS
    kafka_broker_list = 'kafka:9093',
    kafka_num_consumers = 1,
    kafka_topic_list = 'flows',
    kafka_group_name = 'clickhouse',
    kafka_format = 'Protobuf',
    kafka_schema = 'flow.proto:FlowMessage';

CREATE TABLE IF NOT EXISTS flows_raw
(
    date Date,

    type Int32,
    time_received DateTime64(9),

    sequence_num UInt32,
    sampling_rate UInt64,
    sampler_address FixedString(16),

    time_flow_start DateTime64(9),
    time_flow_end DateTime64(9),

    bytes UInt64,
    packets UInt64,

    src_addr FixedString(16),
    dst_addr FixedString(16),

    src_as UInt32,
    dst_as UInt32,

    etype UInt32,
    proto UInt32,

    src_port UInt32,
    dst_port UInt32,

    forwarding_status UInt32,
    tcp_flags UInt32,
    icmp_type UInt32,
    icmp_code UInt32,

    fragment_id UInt32,
    fragment_offset UInt32
)
ENGINE = MergeTree()
PARTITION BY date
ORDER BY time_received;

CREATE FUNCTION IF NOT EXISTS convertFixedStringIpToString AS (etype, addr) ->
(
    if(etype = 0x0800, IPv4NumToString(reinterpretAsUInt32(substring(reverse(addr), 13,4))), IPv6NumToString(addr))
);

CREATE MATERIALIZED VIEW IF NOT EXISTS flows_raw_mv TO flows_raw AS
    SELECT
        toDate(time_received_ns / 1000000000) AS date,
        type,
        toDateTime64(time_received_ns / 1000000000, 9) AS time_received,

        sequence_num,
        sampling_rate,
        sampler_address,

        toDateTime64(time_flow_start_ns / 1000000000, 9) AS time_flow_start,
        toDateTime64(time_flow_end_ns / 1000000000, 9) AS time_flow_end,

        bytes,
        packets,

        src_addr,
        dst_addr,

        src_as,
        dst_as,

        etype,
        proto,

        src_port,
        dst_port,

        forwarding_status,
        tcp_flags,
        icmp_type,
        icmp_code,

        fragment_id,
        fragment_offset
    FROM flows;

CREATE TABLE IF NOT EXISTS flows_5m
(
    date Date,
    timeslot DateTime,

    src_as UInt32,
    dst_as UInt32,

    etypeMap Nested (
        etype UInt32,
        bytes UInt64,
        packets UInt64,
        count UInt64
    ),

    bytes UInt64,
    packets UInt64,
    count UInt64
) ENGINE = SummingMergeTree()
PARTITION BY date
ORDER BY (date, timeslot, src_as, dst_as, `etypeMap.etype`);

CREATE MATERIALIZED VIEW IF NOT EXISTS flows_5m_view TO flows_5m
AS
SELECT
    date,
    toStartOfFiveMinute(time_received) AS timeslot,
    src_as,
    dst_as,

    [etype] AS `etypeMap.etype`,
    [bytes] AS `etypeMap.bytes`,
    [packets] AS `etypeMap.packets`,
    [count] AS `etypeMap.count`,

    sum(bytes) AS bytes,
    sum(packets) AS packets,
    count() AS count
FROM flows_raw
GROUP BY date, timeslot, src_as, dst_as, `etypeMap.etype`;

CREATE VIEW IF NOT EXISTS flows_raw_view AS
    SELECT
        date,

        transform(type, [0, 1, 2, 3, 4], ['unknown', 'sflow_5', 'netflow_v5', 'netflow_v9', 'ipfix'], toString(type)) AS type,
        toStartOfSecond(time_received) AS time_received,

        sequence_num,
        sampler_address,

        toStartOfSecond(time_flow_start) AS time_flow_start,
        toStartOfSecond(time_flow_end) AS time_flow_end,

        bytes * max2(sampling_rate, 1) AS bytes,
        packets * max2(sampling_rate, 1) AS packets,

        convertFixedStringIpToString(etype, src_addr) AS src_addr,
        convertFixedStringIpToString(etype, dst_addr) AS dst_addr,

        src_as,
        dst_as,

        etype,
        dictGetOrDefault('protocol_dictionary', 'name', proto, toString(proto)) AS proto,

        proto || '/' || toString(src_port) as src_port,
        proto || '/' || toString(dst_port) as dst_port,

        transform(forwarding_status, [0, 1, 2, 3], ['unknown', 'forwarded', 'dropped', 'consumed'], toString(forwarding_status)) AS forwarding_status,
        arrayMap(x -> transform(x, [1, 2, 4, 8, 16, 32, 64, 128, 256, 512], ['fin', 'syn', 'rst', 'psh', 'ack', 'urg', 'ecn', 'cwr', 'nonce', 'reserved'], toString(x)), bitmaskToArray(tcp_flags)) as tcp_flags,
        icmp_type,
        icmp_code,

        fragment_id,
        fragment_offset
    FROM flows_raw
    ORDER BY time_received DESC;
