# Mapping and Configuration

GoFlow2 allows users to collect and represent non-standard fields
without having to rely on `-produce=raw` setting.

By default, commonly used types are collected into the protobuf.
For instance source and destination IP addresses, TCP/UDP ports, etc.
When suggesting a new field to collect, preference should be given to fields
that are both widely adopted and supported by multiple protocols (sFlow, IPFIX).

Some scenarios require more flexibility.
In fact, IPFIX allows Private Enterprise Numbers ([PEN](https://www.iana.org/assignments/enterprise-numbers/))
and entire datagrams (IPFIX, sFlow) can contain bytes of interest.

A mapping configuration file empowers GoFlow2 users to collect
extra data without changing the code and recompiling.
The feature is available for both protobuf binary and JSON formatting.

A configuration file can be invoked the following way:

```bash
goflow2 -mapping=config.yaml -format=json -produce=sample
```

An example configuration file that collects NetFlow/IPFIX flow direction information: 

```yaml
formatter:
  fields: # list of fields to format in JSON
    - flow_direction
  protobuf: # manual protobuf fields addition
    - name: flow_direction
      index: 42
      type: varint
# Decoder mappings
ipfix:
  mapping:
    - field: 61
      destination: flow_direction
netflowv9:
  mapping:
    - field: 61
      destination: flow_direction
```

In JSON, the field `flow_direction` will now be added.
In binary protobuf, when consumed by another tool,
the latter can access the new field at index 42.
A custom proto file can be compiled with the following:

```proto
message FlowMessage {

  ...
  uint32 flow_direction = 42;

```

## Formatting and rendering

This section of the configuration is used for textual representations.
Both fields from [`flow.proto`](../pb/flow.proto) and custom ones inside `formatter.protobuf`
can be available in the textual output (JSON for instance).

The items inside `formatter.fields` are the fields present in the output.

The render section picks the representation.
For instance a 4/16 bytes field can be represented as an IP address, time can be represented as RFC3339 or epoch.

```yaml
formatter:
  fields:
    - time_received_ns
    - my_new_field
    - my_other_field
  protobuf:
    - name: my_new_field
      index: 1000
      type: varint
    - name: my_other_field
      index: 2000
      type: string
  render:
    time_received_ns: datetimenano
    my_other_field: ip
```

## Encapsulation

Custom mapping can be used with encapsulation.

By default, GoFlow2 will expect a packet with the following layers:

* Ethernet
* 802.1q and/or MPLS
* IP
* TCP or UDP

A more complex packet could be in the form:

* **Ethernet**
* **MPLS**
* **IP**
* *GRE*
* *Ethernet*
* *IP*
* *UDP*

Only the layers in **bold** will have the information collected.
The perimeter that is considered encapsulation here is the GRE protocol (note: it could be started if a second Ethernet layer was above 802.1q).
Rather than having duplicates of the existing fields with encapsulation, a configuration file can be used to collect
the encapsulated fields.

An additional consideration is that protobuf fields can be array (or `repeated`).
Due to the way the mapping works, the arrays are not [packed](https://protobuf.dev/programming-guides/encoding/#packed)
(equivalent to a `repeated myfield = 123 [packed=false]` in the definition).
Each item is encoded in the order they are parsed alongside other fields
whereas packed would require a second pass to combine all the items together.

### Inner UDP/TCP ports

```yaml
formatter:
  fields:
    - src_port_encap
    - dst_port_encap
  protobuf:
    - name: src_port_encap
      index: 1021
      type: string
      array: true
    - name: dst_port_encap
      index: 1022
      type: string
      array: true
sflow:
  mapping:
    - layer: "udp"
      encap: true
      offset: 0
      length: 16
      destination: src_port_encap
    - layer: "udp"
      encap: true
      offset: 16
      length: 16
      destination: dst_port_encap
    - layer: "tcp"
      encap: true
      offset: 0
      length: 16
      destination: src_port_encap
    - layer: "tcp"
      encap: true
      offset: 16
      length: 16
      destination: dst_port_encap
```

### Inner IP addresses

```yaml
formatter:
  fields:
    - src_ip_encap
    - dst_ip_encap
  protobuf:
    - name: src_ip_encap
      index: 1006
      type: string
      array: true
    - name: dst_ip_encap
      index: 1007
      type: string
      array: true
  render:
    src_ip_encap: ip
    dst_ip_encap: ip
sflow:
  mapping:
    - layer: "ipv6"
      encap: true
      offset: 64
      length: 128
      destination: src_ip_encap
    - layer: "ipv6"
      encap: true
      offset: 192
      length: 128
      destination: dst_ip_encap
    - layer: "ipv4"
      encap: true
      offset: 96
      length: 32
      destination: src_ip_encap
    - layer: "ipv4"
      encap: true
      offset: 128
      length: 32
      destination: dst_ip_encap
```
