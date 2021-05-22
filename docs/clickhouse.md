# Flows and Clickhouse

Clickhouse is a powerful data warehouse.

A sample [docker-compose](../compose/docker-compose.yml) is provided.
It's composed of:
* Apache Kafka
* Apache Zookeeper
* GoFlow2
* Prometheus
* Clickhouse
* Grafana

To start the containers, use:
```bash
$ docker-compose up
```

This command will automatically build Grafana and GoFlow2 containers.

GoFlow2 collects NetFlow v9/IPFIX and sFlow packets and sends as a protobuf into Kafka.
Zookeeper coordinates Kafka and can also be used by Clickhouse to ensure replication.
Prometheus scrapes the metrics of the collector.
Clickhouse consumes from Kafka, stores raw data and aggregates over specific columns
using `MATERIALIZED TABLES` and `VIEWS` defined in a [schema file](../compose/clickhouse/create.sh).
Youj can visualize the data in Grafana (credentials: admin/admin) with the
pre-made dashboards.