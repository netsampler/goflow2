# Protocols

You can find information on the protocols:
* [sFlow](https://sflow.org/developers/specifications.php)
* [NetFlow v5](https://www.cisco.com/c/en/us/td/docs/net_mgmt/netflow_collection_engine/3-6/user/guide/format.html)
* [NetFlow v9](https://www.cisco.com/en/US/technologies/tk648/tk362/technologies_white_paper09186a00800a3db9.html)
* [IPFIX](https://www.iana.org/assignments/ipfix/ipfix.xhtml)

The mapping to the protobuf format is listed in the table below.

| Field | Description | NetFlow v5 | sFlow | NetFlow v9 | IPFIX |
| - | - | - | - | - | - |
|Type|Type of flow message|NETFLOW_V5|SFLOW_5|NETFLOW_V9|IPFIX|
|time_received_ns|Timestamp in nanoseconds of when the message was received|Included|Included|Included|Included|
|sequence_num|Sequence number of the flow packet|Included|Included|Included|Included|
|sampling_rate|Sampling rate of the flow|Included|Included|Included|Included|
|sampler_address|Address of the device that generated the packet|IP source of packet|Agent IP|IP source of packet|IP source of packet|
|time_flow_start_ns|Time the flow started in nanoseconds|System uptime and first|=TimeReceived|System uptime and FIRST_SWITCHED (22)|flowStartXXX (150, 152, 154, 156)|
|time_flow_end_ns|Time the flow ended in nanoseconds|System uptime and last|=TimeReceived|System uptime and LAST_SWITCHED (23)|flowEndXXX (151, 153, 155, 157)|
|bytes|Number of bytes in flow|dOctets|Length of sample|IN_BYTES (1) OUT_BYTES (23)|octetDeltaCount (1) postOctetDeltaCount (23)|
|packets|Number of packets in flow|dPkts|=1|IN_PKTS (2) OUT_PKTS (24)|packetDeltaCount (2) postPacketDeltaCount (24)|
|src_addr|Source address (IP)|srcaddr (IPv4 only)|Included|Included|IPV4_SRC_ADDR (8) IPV6_SRC_ADDR (27)|sourceIPv4Address/sourceIPv6Address (8/27)|
|dst_addr|Destination address (IP)|dstaddr (IPv4 only)|Included|Included|IPV4_DST_ADDR (12) IPV6_DST_ADDR (28)|destinationIPv4Address (12)destinationIPv6Address (28)|
|etype|Ethernet type (0x86dd for IPv6...)|IPv4|Included|Included|Included|
|proto|Protocol (UDP, TCP, ICMP...)|prot|Included|PROTOCOL (4)|protocolIdentifier (4)|
|src_port|Source port (when UDP/TCP/SCTP)|srcport|Included|L4_SRC_PORT (7)|sourceTransportPort (7)|
|dst_port|Destination port (when UDP/TCP/SCTP)|dstport|Included|L4_DST_PORT (11)|destinationTransportPort (11)|
|in_if|Input interface|input|Included|INPUT_SNMP (10)|ingressInterface (10)|
|out_if|Output interface|output|Included|OUTPUT_SNMP (14)|egressInterface (14)|
|src_mac|Source mac address| |Included|IN_SRC_MAC (56)|sourceMacAddress (56)|
|dst_mac|Destination mac address| |Included|OUT_DST_MAC (57)|postDestinationMacAddress (57)|
|src_vlan|Source VLAN ID| |From ExtendedSwitch|SRC_VLAN (58)|vlanId (58)|
|dst_vlan|Destination VLAN ID| |From ExtendedSwitch|DST_VLAN (59)|postVlanId (59)|
|vlan_id|802.11q VLAN ID| |Included|SRC_VLAN (58)|vlanId (58)|
|ip_tos|IP Type of Service|tos|Included|SRC_TOS (5)|ipClassOfService (5)|
|forwarding_status|Forwarding status| | |FORWARDING_STATUS (89)|forwardingStatus (89)|
|ip_ttl|IP Time to Live| |Included|IPTTL (52)|minimumTTL (52|
|tcp_flags|TCP flags|tcp_flags|Included|TCP_FLAGS (6)|tcpControlBits (6)|
|icmp_type|ICMP Type| |Included|ICMP_TYPE (32)|icmpTypeXXX (176, 178) icmpTypeCodeXXX (32, 139)|
|icmp_code|ICMP Code| |Included|ICMP_TYPE (32)|icmpCodeXXX (177, 179) icmpTypeCodeXXX (32, 139)|
|ipv6_flow_label|IPv6 Flow Label| |Included|IPV6_FLOW_LABEL (31)|flowLabelIPv6 (31)|
|fragment_id|IP Fragment ID| |Included|IPV4_IDENT (54)|fragmentIdentification (54)|
|fragment_offset|IP Fragment Offset| |Included|FRAGMENT_OFFSET (88)|fragmentOffset (88) and fragmentFlags (197)|
|src_as|Source AS number|src_as|From ExtendedGateway|SRC_AS (16)|bgpSourceAsNumber (16)|
|dst_as|Destination AS number|dst_as|From ExtendedGateway|DST_AS (17)|bgpDestinationAsNumber (17)|
|next_hop|Nexthop address|nexthop|From ExtendedRouter|IPV4_NEXT_HOP (15) IPV6_NEXT_HOP (62)|ipNextHopIPv4Address (15) ipNextHopIPv6Address (62)|
|next_hop_as|Nexthop AS number| |From ExtendedGateway| | |
|src_net|Source address mask|src_mask|From ExtendedRouter|SRC_MASK (9) IPV6_SRC_MASK (29)|sourceIPv4PrefixLength (9) sourceIPv6PrefixLength (29)|
|dst_net|Destination address mask|dst_mask|From ExtendedRouter|DST_MASK (13) IPV6_DST_MASK (30)|destinationIPv4PrefixLength (13) destinationIPv6PrefixLength (30)|
|bgp_next_hop|BGP Nexthop address| |From ExtendedGateway|BGP_IPV4_NEXT_HOP (18) BGP_IPV6_NEXT_HOP (63)|bgpNextHopIPv4Address (18) bgpNextHopIPv6Address (63)|
|bgp_communities|BGP Communities| |From ExtendedGateway| | |
|as_path|AS Path| |From ExtendedGateway| | |destinationIPv6PrefixLength (30)|
|mpls_ttl|TTL of the MPLS label||Included|||
|mpls_label|MPLS label list||Included|||

## Producers

When using the **raw** producer, you can access a sample:

```bash
$ go run main.go -produce raw -format json
```

This can be useful if you need to debug received packets
or looking to dive into a specific protocol (eg: the sFlow counters).

```json
{
    "type": "sflow",
    "message":
    {
        "version": 5,
        "ip-version": 1,
        "agent-ip": "127.0.0.1",
        "sub-agent-id": 100000,
        "sequence-number": 1234,
        "uptime": 19070720,
        "samples-count": 1,
        "samples":
        [
            {
                "header":
                {
                    "format": 2,
                    "length": 124,
                    "sample-sequence-number": 340,
                    "source-id-type": 0,
                    "source-id-value": 6
                },
                "counter-records-count": 1,
                "records":
                [
                    {
                        "header":
                        {
                            "data-format": 1,
                            "length": 88
                        },
                        "data":
                        {
                            "if-index": 6,
                            "if-type": 6,
                            "if-speed": 0,
                            "if-direction": 0,
                            "if-status": 3,
                            "if-in-octets": 0,
                            "if-in-ucast-pkts": 1000,
                            "if-in-multicast-pkts": 0,
                            "if-in-broadcast-pkts": 0,
                            "if-in-discards": 0,
                            "if-in-errors": 0,
                            "if-in-unknown-protos": 0,
                            "if-out-octets": 0,
                            "if-out-ucast-pkts": 2000,
                            "if-out-multicast-pkts": 0,
                            "if-out-broadcast-pkts": 0,
                            "if-out-discards": 0,
                            "if-out-errors": 0,
                            "if-promiscuous-mode": 0
                        }
                    }
                ]
            }
        ]
    },
    "src": "[::ffff:127.0.0.1]:50001",
    "time_received": "2023-04-15T20:44:42.723694Z"
}
```

When using the **Protobuf** producer, you have access to various configuration options.
The [`mapping.yaml`](../cmd/goflow2/mapping.yaml) file can be used with `-mapping=mapping.yaml` in the CLI.

It enables features like:
* Add protobuf fields
* Renaming fields (JSON/text)
* Hashing key (for Kafka)
* Mapping new values from samples

For example, you can rename:

```yaml
formatter:
  rename: # only for JSON/text
    src_mac: src_macaddr
    dst_mac: dst_macaddr
```

### Columns and renderers

By default, all the columns above will be printed when using JSON or text.
To restrict to a subset of columns, in the mapping file, list the ones you want:

```yaml
formatter:
  fields:
    - src_addr
```

There is a support for virtual columns (eg: `icmp_name`).

Renderers are a special handling of fields:

```yaml
formatter:
  render:
    src_mac: mac
    dst_mac: mac
    dst_net: none # overrides: render the network as integer instead of prefix based on src/dst addr
```

You can assign a specific formatter.

### Map custom fields

If you are using enterprise fields that you need decoded or if you are looking for specific bytes inside the packet sample.

Data coming from the flows can be added to the protobuf either as an unsigned/signed integer a slice of bytes.

The `sflow` section allow to extract data from packet samples inside sFlow and inside IPFIX (dataframe).
The following layers are available:
* 0: no offset
* 3, ipv4, ipv6, arp: network layer, offsets to IP/IPv6 header
* 4, icmp, icmp6, udp, tcp: transport layer, offsets to TCP/UDP/ICMP header
* 7: application layer, offsets to the TCP/UDP payload

The data extracted will then be added to either an existing field (see samping rate below),
or to a newly defined field.

In order to display them with JSON or text, you need to specify them in `fields`.

```yaml
formatter:
  fields:
    - sampling_rate
    - custom_src_port
    - juniper_properties
  protobuf:
    - name: juniper_properties
      index: 1001
      type: varint
      array: true
ipfix:
  mapping:
    - field: 34 # samplingInterval provided within the template
      destination: sampling_rate
      endian: little # special endianness

    - field: 137 # Juniper Properties
      destination: juniper_properties
      penprovided: true # has an enterprise number
      pen: 2636 # Juniper enterprise
netflowv9:
  mapping: []
    # ... similar to above but the enterprise number will not be supported
sflow:
  mapping: # also inside an IPFIX dataFrame
    - layer: "4" # Layer
      offset: 0 # Source port
      length: 16 # 2 bytes
      destination: custom_src_port
```

Another example if you wish to decode the TTL from the IP:

```yaml
formatter:
  protobuf: # manual protobuf fields addition
    - name: egress_vrf_id
      index: 40
      type: varint
ipfix:
  mapping:
    - field: 51
      destination: ip_ttl_test
netflowv9:
  mapping:
    - field: 51
      destination: ip_ttl_test
sflow:
  mapping:
    - layer: "ipv4"
      offset: 64
      length: 8
      destination: ip_ttl_test
    - layer: "ipv6"
      offset: 56
      length: 8
      destination: ip_ttl_test
```
