FROM ubuntu AS builder

RUN apt-get update && apt-get install -y git
RUN git clone https://github.com/Vertamedia/clickhouse-grafana.git

FROM grafana/grafana:9.1.7

COPY --from=builder /clickhouse-grafana /var/lib/grafana/plugins