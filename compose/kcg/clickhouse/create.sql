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
    dst_port UInt32
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
    dst_port UInt32
)
ENGINE = MergeTree()
PARTITION BY toDate(time_received)
ORDER BY time_received;

CREATE MATERIALIZED VIEW IF NOT EXISTS flows_raw_mv TO flows_raw AS
    SELECT
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
        dst_port
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
    toDate(time_received) AS date,
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

CREATE FUNCTION IF NOT EXISTS convertFixedStringIpToString AS (addr) ->
(
    -- if the first 12 bytes are zero, then it's an IPv4 address, otherwise it's an IPv6 address
    if(reinterpretAsUInt128(substring(reverse(addr), 1, 12)) = 0, IPv4NumToString(reinterpretAsUInt32(substring(reverse(addr), 13, 4))), IPv6NumToString(addr))
);

CREATE VIEW IF NOT EXISTS flows_raw_view AS
    SELECT
        transform(type, [0, 1, 2, 3, 4], ['unknown', 'sflow_5', 'netflow_v5', 'netflow_v9', 'ipfix'], toString(type)) AS type,
        time_received,

        sequence_num,
        convertFixedStringIpToString(sampler_address) AS sampler_address,

        time_flow_start,
        time_flow_end,

        bytes * max2(sampling_rate, 1) AS bytes,
        packets * max2(sampling_rate, 1) AS packets,

        convertFixedStringIpToString(src_addr) AS src_addr,
        convertFixedStringIpToString(dst_addr) AS dst_addr,

        src_as,
        dst_as,

        etype,
        dictGetOrDefault('protocol_dictionary', 'name', proto, toString(proto)) AS proto,

        src_port,
        dst_port
    FROM flows_raw
    ORDER BY time_received DESC;
