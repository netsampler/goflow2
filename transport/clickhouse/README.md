# ClickHouse Transport User Guide

## Overview

The ClickHouse transport module for GoFlow2 provides high-performance storage and analysis of network flow data in ClickHouse databases. It supports clustering, batching, enrichment, and real-time metrics.

## Architecture

The ClickHouse transport consists of several key components:

- **Transport Driver**: Main orchestrator ([`transport.go`](transport/clickhouse/transport.go))
- **Database Layer**: Connection pooling and batch operations ([`db/`](transport/clickhouse/db/))
- **Configuration**: Environment and CLI configuration ([`config/`](transport/clickhouse/config/))
- **Queue System**: Memory-efficient FIFO queue ([`queue/`](transport/clickhouse/queue/))
- **Enrichment**: Pluggable data enrichment system ([`enricher/`](transport/clickhouse/enricher/))

## Quick Start

### 1. Database Setup

First, create the required ClickHouse tables using the provided SQL schema:

```sql
-- Create database
CREATE DATABASE IF NOT EXISTS flows ON CLUSTER your_cluster


-- Create local table (replace cluster name)
CREATE TABLE flows.flows_local ON CLUSTER your_cluster
(
    `ts` DateTime MATERIALIZED now(),
    `date` Date MATERIALIZED toDate(ts),
    `type` UInt32,
    `time_received_ns` UInt64,
    -- ... (see transport/clickhouse/SQL/db.sql for complete schema)
)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/{database}/{table}', '{replica}')
PARTITION BY toStartOfHour(ts)
ORDER BY ts
TTL ts + toIntervalHour(1)
SETTINGS index_granularity = 8192;

-- Create distributed table
CREATE TABLE flows.flows ON CLUSTER your_cluster AS flows.flows_local 
ENGINE = Distributed('your_cluster', 'flows', 'flows_local', rand());
```

### 2. Configuration

Configure the transport using environment variables or command-line flags:

#### Environment Variables

```bash
# Database connection
export TRANSPORT_CLICKHOUSE_SRV="clickhouse1:9000,clickhouse2:9000"
export TRANSPORT_CLICKHOUSE_DATABASE="flows"
export TRANSPORT_CLICKHOUSE_USER="goflow2"
export TRANSPORT_CLICKHOUSE_PASSWORD="your_password"
export TRANSPORT_CLICKHOUSE_CLUSTER="your_cluster"

# Performance tuning
export TRANSPORT_CLICKHOUSE_BATCH_SIZE="200000"
export TRANSPORT_CLICKHOUSE_INSERT_INTERVAL="1s"
export TRANSPORT_CLICKHOUSE_MAX_PARALLEL_INSERTS="5"
export TRANSPORT_CLICKHOUSE_BATCH_INSERT_TH="0.7"

# Enrichment (optional)
export TRANSPORT_CLICKHOUSE_ENRICHERS="example"
```

#### Command-Line Flags

```bash
goflow2 \
  -produce="sample" \
  -format="bin" \
  -transport="clickhouse" \
  -transport.ch.servers="clickhouse1:9440,clickhouse2:9440" \
  -transport.ch.cluster="your_cluster" \
  -transport.ch.db="flows" \
  -transport.ch.user="goflow2" \
  -transport.ch.password="your_password"
```

## Configuration Reference

### Database Configuration

| Parameter | Environment Variable | Flag | Default | Description |
|-----------|---------------------|------|---------|-------------|
| Servers | `TRANSPORT_CLICKHOUSE_SRV` | `-transport.ch.servers` | - | Comma-separated ClickHouse server addresses |
| Database | `TRANSPORT_CLICKHOUSE_DATABASE` | `-transport.ch.db` | - | Database name |
| User | `TRANSPORT_CLICKHOUSE_USER` | `-transport.ch.user` | - | Database username |
| Password | `TRANSPORT_CLICKHOUSE_PASSWORD` | `-transport.ch.password` | - | Database password |
| Cluster | `TRANSPORT_CLICKHOUSE_CLUSTER` | `-transport.ch.cluster` | - | ClickHouse cluster name |

### Performance Configuration

| Parameter | Environment Variable | Flag | Default | Description |
|-----------|---------------------|------|---------|-------------|
| Batch Size | `TRANSPORT_CLICKHOUSE_BATCH_SIZE` | `-transport.ch.batch.size` | 200,000 | Maximum records per batch |
| Insert Interval | `TRANSPORT_CLICKHOUSE_INSERT_INTERVAL` | `-transport.ch.insert.interval` | 1s | Time between insert attempts |
| Max Parallel Inserts | `TRANSPORT_CLICKHOUSE_MAX_PARALLEL_INSERTS` | `-transport.ch.insert.parallel` | 5 | Maximum concurrent insert operations |
| Batch Threshold | `TRANSPORT_CLICKHOUSE_BATCH_INSERT_TH` | `-transport.ch.batch.insert.th` | 0.7 | Queue fill ratio to trigger insert |
| Force Insert After | `TRANSPORT_CLICKHOUSE_FORCE_INSERT_AFTER` | `-transport.ch.force.insert.after` | 60 | Insert attempts before ignoring batch size |

### Enrichment Configuration

| Parameter | Environment Variable | Flag | Default | Description |
|-----------|---------------------|------|---------|-------------|
| Enrichers | `TRANSPORT_CLICKHOUSE_ENRICHERS` | `-transport.ch.enrichers` | - | Comma-separated list of enricher names |

## Features

### 1. Connection Pooling

The transport automatically manages connections to multiple ClickHouse servers:

- **Cluster Discovery**: Automatically discovers all nodes in a ClickHouse cluster
- **Load Balancing**: Randomly distributes connections across available servers
- **Health Monitoring**: Tracks server health and automatically recovers failed connections
- **Connection Reuse**: Efficiently reuses connections to minimize overhead

### 2. Intelligent Batching

Optimizes insert performance through smart batching:

- **Threshold-based Inserts**: Triggers inserts when queue reaches specified capacity
- **Parallel Processing**: Supports multiple concurrent insert operations

### 3. Memory Management

Efficient memory usage through:

- **Size-Limited Queue**: Prevents memory exhaustion with configurable limits
- **Memory Metrics**: Real-time monitoring of queue memory usage
- **Buffer Pooling**: Reuses byte buffers to reduce garbage collection

### 4. Data Enrichment

Extensible enrichment system:

- **Pluggable Architecture**: Easy to add custom enrichers
- **Error Handling**: Graceful handling of enrichment failures
- **Hot Reloading**: Enrichers can be reinitialized without restart

### 5. Monitoring & Metrics

Comprehensive observability:

- **Prometheus Metrics**: Built-in metrics for monitoring
- **Structured Logging**: JSON-structured logs with context
- **Health Checks**: Real-time status monitoring
- **Performance Metrics**: Insert timing, queue statistics, error rates

## Metrics

The transport exposes the following Prometheus metrics:

### Queue Metrics
- `ch_queue_mem_avail`: Available queue memory
- `ch_queue_mem_used`: Used queue memory  
- `ch_queue_records_queued`: Number of queued records
- `ch_queue_records_dropped`: Number of dropped records

### Insert Metrics
- `ch_insert_time`: Insert operation duration
- `ch_insert_err`: Number of insert errors
- `ch_insert_job_runned`: Number of insert jobs executed
- `ch_insert_job_skipped_err`: Number of skipped insert jobs
- `ch_batch_size`: Current batch size

### Processing Metrics
- `ch_unmarshall_err`: Unmarshalling errors
- `ch_queue_add_err`: Queue addition errors

## Troubleshooting

### Common Issues

1. **Connection Failures**
   - Verify ClickHouse server addresses and ports
   - Check network connectivity and firewall rules
   - Ensure credentials are correct

2. **High Memory Usage**
   - Reduce batch size or increase insert frequency
   - Monitor queue metrics for memory pressure
   - Check for slow ClickHouse performance

3. **Insert Errors**
   - Verify table schema matches flow record structure
   - Check ClickHouse server logs for detailed errors
   - Monitor cluster health and disk space

4. **Performance Issues**
   - Tune batch size based on your data volume
   - Adjust parallel insert count based on ClickHouse capacity
   - Monitor insert timing metrics

### Health Monitoring

Monitor the transport health through:

- **Metrics**: Monitor Prometheus metrics for performance indicators
- **Logs**: Check structured logs for operational details

## Best Practices

1. **Sizing Guidelines**
   - Start with default batch size (200,000) and adjust based on performance
   - Use 1-5 parallel inserts depending on ClickHouse cluster size
   - Set insert interval to 1-5 seconds based on latency requirements

2. **Cluster Configuration**
   - Use ReplicatedMergeTree for high availability
   - Configure appropriate TTL for data retention
   - Use distributed tables for query performance

3. **Monitoring**
   - Set up alerts on error metrics and queue memory usage
   - Monitor insert timing for performance degradation
   - Track cluster health and connection status

4. **Security**
   - Use strong passwords and consider certificate-based authentication
   - Restrict network access to ClickHouse servers
   - Regularly rotate credentials

## Advanced Configuration

### Custom Enrichers

Create custom enrichers by implementing the [`Enricher`](transport/clickhouse/common/enricher.go:14) interface:

```go
type Enricher interface {
    Proceed(record EnrichmentRecord) error
    Init(ctx context.Context) error
    Name() string
}
```

Register your enricher using [`RegisterEnricherFabricFunc`](transport/clickhouse/common/enricher.go:21).

### Performance Tuning

For high-throughput environments:

1. **Increase Batch Size**: Up to 500,000+ records for high-volume scenarios
2. **Optimize Insert Interval**: Balance between latency and efficiency
3. **Scale Parallel Inserts**: Match ClickHouse cluster capacity
4. **Tune Queue Size**: Adjust based on memory constraints and burst capacity

### Schema Customization

The default schema in [`SQL/db.sql`](transport/clickhouse/SQL/db.sql) can be customized:

- Add custom fields for enriched data
- Modify TTL settings for retention requirements
- Adjust partitioning strategy for query performance
- Configure compression for storage optimization
