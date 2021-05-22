# GoFlow2

[![Build Status](https://github.com/netsampler/goflow2/workflows/Build/badge.svg)](https://github.com/netsampler/goflow2/actions?query=workflow%3ABuild)
[![Go Reference](https://pkg.go.dev/badge/github.com/netsampler/goflow2.svg)](https://pkg.go.dev/github.com/netsampler/goflow2)

This application is a NetFlow/IPFIX/sFlow collector in Go.

It gathers network information (IP, interfaces, routers) from different flow protocols,
serializes it in a common format.

You will want to use GoFlow if:
* You receive a decent amount of network samples and need horizontal scalability
* Have protocol diversity and need a consistent format
* Require raw samples and build aggregation and custom enrichment

This software is the entry point of a pipeline. The storage, transport, enrichment, graphing, alerting are
not provided.

![GoFlow2 System diagram](/graphics/diagram.png)

## Origins

This work is a fork of a previous [open-source GoFlow code](https://github.com/cloudflare/goflow) built and used at Cloudflare.
It lives in its own GitHub organization to be maintained more easily.

Among the differences with the original code:
The serializer and transport options have been revamped to make this program more user friendly.
and target new use-cases like logging providers.
Minimal changes in the decoding libraries.

## Modularity

In order to enable load-balancing and optimizations, the GoFlow library has a `decoder` which converts
the payload of a flow packet into a Go structure.

The `producer` functions (one per protocol) then converts those structures into a protobuf (`pb/flow.pb`)
which contains the fields a network engineer is interested in.
The flow packets usually contains multiples samples
This acts as an abstraction of a sample.

The `format` directory offers various utilities to process the protobuf. It can convert

The `transport` provides different way of processing the protobuf. Either sending it via Kafka or 
send it to a file (or stdout).

GoFlow2 is a wrapper of all the functions and chains thems.

You can build your own collector using this base and replace parts:
* Use different transport (eg: RabbitMQ instead of Kafka)
* Convert to another format (eg: Cap'n Proto, Avro, instead of protobuf)
* Decode different samples (eg: not only IP networks, add MPLS)
* Different metrics system (eg: [OpenTelemetry](https://opentelemetry.io/))

### Protocol difference

The sampling protocols have distinct features:

**sFlow** is a stateless protocol which sends the full header of a packet with router information
(interfaces, destination AS) while **NetFlow/IPFIX** rely on templates that contain fields (eg: source IPv6).

The sampling rate in NetFlow/IPFIX is provided by **Option Data Sets**. This is why it can take a few minutes
for the packets to be decoded until all the templates are received (**Option Template** and **Data Template**).

Both of these protocols bundle multiple samples (**Data Set** in NetFlow/IPFIX and **Flow Sample** in sFlow)
in one packet.

The advantages of using an abstract network flow format, such as protobuf, is it enables summing over the
protocols (eg: per ASN or per port, rather than per (ASN, router) and (port, router)).

To read more about the protocols and how they are mapped inside, check out [page](/docs/protocols.md)

### Features of GoFlow2

Collection:
* NetFlow v5
* IPFIX/NetFlow v9 (sampling rate provided by the Option Data Set)
* sFlow v5

(adding NetFlow v1,7,8 is being evaluated)

Production:
* Convert to protobuf or json
* Prints to the console/file
* Sends to Kafka and partition

Monitoring via Prometheus metrics

## Get started

To read about agents that samples network traffic, check this [page](/docs/agents.md).

To setup the collector, download the latest release corresponding to your OS
and run the following command (the binaries have a suffix with the version):

```bash
$ ./goflow2
```

By default, this command will launch an sFlow collector on port `:6343` and
a NetFlowV9/IPFIX collector on port `:2055`.

By default, the samples received will be printed in JSON format on the stdout.

```json
{
  "Type": "SFLOW_5",
  "TimeFlowEnd": 1621820000,
  "TimeFlowStart": 1621820000,
  "TimeReceived": 1621820000,
  "Bytes": 70,
  "Packets": 1,
  "SamplingRate": 100,
  "SamplerAddress": "192.168.1.254",
  "DstAddr": "10.0.0.1",
  "DstMac": "ff:ff:ff:ff:ff:ff",
  "SrcAddr": "192.168.1.1",
  "SrcMac": "ff:ff:ff:ff:ff:ff",
  "InIf": 1,
  "OutIf": 2,
  "Etype": 2048,
  "EtypeName": "IPv4",
  "Proto": 6,
  "ProtoName": "TCP",
  "SrcPort": 443,
  "DstPort": 46344,
  "FragmentId": 54044,
  "FragmentOffset": 16384,
  ...
  "IPTTL": 64,
  "IPTos": 0,
  "TCPFlags": 16,
}
```

If you are using a log integration (eg: Loki with Promtail, Splunk, Fluentd, Google Cloud Logs, etc.),
just send the output into a file.
```bash
$ ./goflow2 -transport.file /var/logs/goflow2.log
```

To enable Kafka and send protobuf, use the following arguments:
```bash
$ ./goflow2 -transport=kafka -transport.kafka.brokers=localhost:9092 -transport.kafka.topic=flows -format=pb
```

By default, the distribution will be randomized.
To partition the feed (any field of the protobuf is available), the following options can be used:
```
-transport.kafka.hashing=true \
-format.hash=SamplerAddress,DstAS
```

### Docker

You can also run directly with a container:
```
$ sudo docker run -p 6343:6343/udp -p 2055:2055/udp -ti netsampler/goflow2:latest
```

### Output format considerations

The JSON format is advised only when consuming a small amount of data directly.
For bigger workloads, the protobuf output format provides a binary representation
and is preferred.
It can also be extended wtih enrichment as long as the user keep the same IDs.

If you want to develop applications, build `pb/flow.proto` into the language you want:
When adding custom fields, picking a field ID ≥ 1000 is suggested.

You can compile the protobuf using the Makefile for Go.
```
make proto
```

For compiling the protobuf for other languages, refer to the [official guide](https://developers.google.com/protocol-buffers).

## Flow Pipeline

A basic enrichment tool is available in the `cmd/enricher` directory.
You need to load the Maxmind GeoIP ASN and Country databases using `-db.asn` and `-db.country`.

Running a flow enrichment system is as simple as a pipe.
Once you plug the stdin of the enricher to the stdout of GoFlow in protobuf,
the source and destination IP addresses will automatically be mapped 
with a database for Autonomous System Number and Country.
Similar output options as GoFlow are provided.

```bash
$ ./goflow2 -format=pb | ./enricher -db.asn path-to/GeoLite2-ASN.mmdb -db.country path-to/GeoLite2-Country.mmdb
```

For a more scalable production setting, Kafka and protobuf are recommended.
Stream operations (aggregation and filtering) can be done with stream-processor tools.
For instance Flink, or the more recent Kafka Streams and kSQLdb.
Direct storage can be done with [Clickhouse](/docs/clickhouse.md). This database can also create materialized tables.

In some cases, the consumer will require protobuf messages to be prefixed by
length. To do this, use the flag `-format.protobuf.fixedlen=true`.

## User stories

Are you using GoFlow2 in production at scale? Add yourself here!

### Contributions

This project welcomes pull-requests, wether it's documentation,
instrumentation (eg: docker-compose, metrics), internals (protocol libraries),
integration (new CLI feature) or else!
Just make sure to check for the use-cases via an issue.

This software would not exist without the testing and commits from
its users and [contributors](docs/contributors.md).

## License

Licensed under the BSD-3 License.
