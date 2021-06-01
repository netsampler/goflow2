# Flows + Logstash + Elastic + Kibana

Clickhouse is a powerful data warehouse.

A sample [docker-compose](./docker-compose.yml) is provided.
It's composed of:
* GoFlow2
* Logstash
* Elastic
* Kibana

To start the containers, use:
```bash
$ docker-compose up
```

This command will automatically build the GoFlow2 container.

GoFlow2 collects NetFlow v9/IPFIX and sFlow packets and logs them into a file (`/var/log/goflow/goflow.log`).
Logstash collects the log messages, parse the JSON and sends to Elastic.
Kibana can be used to visualize the data. You can access the dashboard at http://localhost:5601.

This stack requires to create an [index pattern](http://localhost:5601/app/management/kibana/indexPatterns/create).
Define the index pattern to be `logstash-*`. Select `@timestamp` to be the time filter.
You can then visualize flows in the [Discover](http://localhost:5601/app/discover) section.