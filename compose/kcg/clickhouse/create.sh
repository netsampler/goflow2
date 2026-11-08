#!/bin/bash
set -e

clickhouse client -n <<-EOSQL

    CREATE DATABASE IF NOT EXISTS dictionaries;

    CREATE DICTIONARY IF NOT EXISTS dictionaries.protocols (
        proto UInt8,
        name String,
        description String
    )
    PRIMARY KEY proto
    LAYOUT(FLAT())
    SOURCE (FILE(path '/var/lib/clickhouse/user_files/protocols.csv' format 'CSVWithNames'))
    LIFETIME(3600);

    CREATE TABLE IF NOT EXISTS flows
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

        bytes UInt64,
        packets UInt64
    ) ENGINE = Kafka()
    SETTINGS
        kafka_broker_list = 'kafka:9092',
        kafka_num_consumers = 1,
        kafka_topic_list = 'flows',
        kafka_group_name = 'clickhouse',
        kafka_format = 'Protobuf',
        kafka_schema = 'flow.proto:FlowMessage';

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
    ) ENGINE = MergeTree()
    PARTITION BY date
    ORDER BY (time_received_ns, sampler_address);
    TTL date + INTERVAL 30 DAY DELETE
    SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

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
    ENGINE = SummingMergeTree()
    PARTITION BY date
    ORDER BY (sampler_address, date, minute, proto, dst_port)
    TTL date + INTERVAL 30 DAY DELETE
    SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

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


EOSQL
