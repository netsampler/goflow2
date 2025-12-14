# Flows + Kafka + Clicklhouse + Grafana + Prometheus

Clickhouse is a powerful data warehouse.

A sample [docker-compose](./docker-compose.yml) is provided.
It's composed of:
* Apache Kafka
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
Prometheus scrapes the metrics of the collector.
Clickhouse consumes from Kafka, stores raw data and aggregates over specific columns
using `MATERIALIZED TABLES` and `VIEWS` defined in a [schema file](./clickhouse/create.sh).

You can visualize the data in Grafana at http://localhost:3000 (credentials: admin/admin) with the
pre-made dashboards.

The Clickhouse database user is `default`/`flow`.

Note: if you are using Colima as the engine, it does not support UDP port forwarding. Flows won't be collected.
It is possible to run GoFlow2 locally and feed into kafka if you add an `/etc/hosts` entry `127.0.0.1 kafka`.
