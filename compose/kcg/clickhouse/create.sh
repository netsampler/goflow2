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
        time_received UInt64,
        time_flow_start_ms UInt64,

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
        kafka_topic_list = 'flows',
        kafka_group_name = 'clickhouse',
        kafka_format = 'Protobuf',
        kafka_schema = 'flow.proto:FlowMessage';

    CREATE TABLE IF NOT EXISTS flows_raw
    (
        date Date,
        time_received DateTime,
        time_flow_start_ms DateTime,

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
    ) ENGINE = MergeTree()
    PARTITION BY date
    ORDER BY time_received;

    CREATE MATERIALIZED VIEW IF NOT EXISTS flows_raw_view TO flows_raw
    AS SELECT
        toDate(time_received) AS date,
        *
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
    ORDER BY (date, timeslot, src_as, dst_as, \`etypeMap.etype\`);

    CREATE MATERIALIZED VIEW IF NOT EXISTS flows_5m_view TO flows_5m
    AS
        SELECT
            date,
            toStartOfFiveMinute(time_received) AS timeslot,
            src_as,
            dst_as,

            [etype] AS \`etypeMap.etype\`,
            [bytes] AS \`etypeMap.bytes\`,
            [packets] AS \`etypeMap.packets\`,
            [count] AS \`etypeMap.count\`,

            sum(bytes) AS bytes,
            sum(packets) AS packets,
            count() AS count

        FROM flows_raw
        GROUP BY date, timeslot, src_as, dst_as, \`etypeMap.etype\`;

EOSQL
