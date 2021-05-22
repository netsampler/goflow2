# Protocols

You can find information on the protocols in the links below:
* [sFlow](https://sflow.org/developers/specifications.php)
* [NetFlow v5](https://www.cisco.com/c/en/us/td/docs/net_mgmt/netflow_collection_engine/3-6/user/guide/format.html)
* [NetFlow v9](https://www.cisco.com/en/US/technologies/tk648/tk362/technologies_white_paper09186a00800a3db9.html)
* [IPFIX](https://www.iana.org/assignments/ipfix/ipfix.xhtml)

The mapping to the protobuf format is listed in the table below.

| Field | Description | NetFlow v5 | sFlow | NetFlow v9 | IPFIX |
| - | - | - | - | - | - |
|Type|Type of flow message|NETFLOW_V5|SFLOW_5|NETFLOW_V9|IPFIX|
|TimeReceived|Timestamp of when the message was received|Included|Included|Included|Included|
|SequenceNum|Sequence number of the flow packet|Included|Included|Included|Included|
|SamplingRate|Sampling rate of the flow|Included|Included|Included|Included|
|FlowDirection|Direction of the flow| | |DIRECTION (61)|flowDirection (61)|
|SamplerAddress|Address of the device that generated the packet|IP source of packet|Agent IP|IP source of packet|IP source of packet|
|TimeFlowStart|Time the flow started|System uptime and first|=TimeReceived|System uptime and FIRST_SWITCHED (22)|flowStartXXX (150, 152, 154, 156)|
|TimeFlowEnd|Time the flow ended|System uptime and last|=TimeReceived|System uptime and LAST_SWITCHED (23)|flowEndXXX (151, 153, 155, 157)|
|Bytes|Number of bytes in flow|dOctets|Length of sample|IN_BYTES (1) OUT_BYTES (23)|octetDeltaCount (1) postOctetDeltaCount (23)|
|Packets|Number of packets in flow|dPkts|=1|IN_PKTS (2) OUT_PKTS (24)|packetDeltaCount (1) postPacketDeltaCount (24)|
|SrcAddr|Source address (IP)|srcaddr (IPv4 only)|Included|Included|IPV4_SRC_ADDR (8) IPV6_SRC_ADDR (27)|sourceIPv4Address/sourceIPv6Address (8/27)|
|DstAddr|Destination address (IP)|dstaddr (IPv4 only)|Included|Included|IPV4_DST_ADDR (12) IPV6_DST_ADDR (28)|destinationIPv4Address (12)destinationIPv6Address (28)|
|Etype|Ethernet type (0x86dd for IPv6...)|IPv4|Included|Included|Included|
|Proto|Protocol (UDP, TCP, ICMP...)|prot|Included|PROTOCOL (4)|protocolIdentifier (4)|
|SrcPort|Source port (when UDP/TCP/SCTP)|srcport|Included|L4_DST_PORT (11)|destinationTransportPort (11)|
|DstPort|Destination port (when UDP/TCP/SCTP)|dstport|Included|L4_SRC_PORT (7)|sourceTransportPort (7)|
|InIf|Input interface|input|Included|INPUT_SNMP (10)|ingressInterface (10)|
|OutIf|Output interface|output|Included|OUTPUT_SNMP (14)|egressInterface (14)|
|SrcMac|Source mac address| |Included|IN_SRC_MAC (56)|sourceMacAddress (56)|
|DstMac|Destination mac address| |Included|OUT_DST_MAC (57)|postDestinationMacAddress (57)|
|SrcVlan|Source VLAN ID| |From ExtendedSwitch|SRC_VLAN (59)|vlanId (58)|
|DstVlan|Destination VLAN ID| |From ExtendedSwitch|DST_VLAN (59)|postVlanId (59)|
|VlanId|802.11q VLAN ID| |Included|SRC_VLAN (59)|postVlanId (59)|
|IngressVrfID|VRF ID| | | |ingressVRFID (234)| 
|EgressVrfID|VRF ID| | | |egressVRFID (235)|
|IPTos|IP Type of Service|tos|Included|SRC_TOS (5)|ipClassOfService (5)|
|ForwardingStatus|Forwarding status| | |FORWARDING_STATUS (89)|forwardingStatus (89)|
|IPTTL|IP Time to Live| |Included|IPTTL (52)|minimumTTL (52|
|TCPFlags|TCP flags|tcp_flags|Included|TCP_FLAGS (6)|tcpControlBits (6)|
|IcmpType|ICMP Type| |Included|ICMP_TYPE (32)|icmpTypeXXX (176, 178) icmpTypeCodeXXX (32, 139)|
|IcmpCode|ICMP Code| |Included|ICMP_TYPE (32)|icmpCodeXXX (177, 179) icmpTypeCodeXXX (32, 139)|
|IPv6FlowLabel|IPv6 Flow Label| |Included|IPV6_FLOW_LABEL (31)|flowLabelIPv6 (31)|
|FragmentId|IP Fragment ID| |Included|IPV4_IDENT (54)|fragmentIdentification (54)|
|FragmentOffset|IP Fragment Offset| |Included|FRAGMENT_OFFSET (88)|fragmentOffset (88) and fragmentFlags (197)|
|BiFlowDirection|BiFlow Identification| | | |biflowDirection (239)|
|SrcAS|Source AS number|src_as|From ExtendedGateway|SRC_AS (16)|bgpSourceAsNumber (16)|
|DstAS|Destination AS number|dst_as|From ExtendedGateway|DST_AS (17)|bgpDestinationAsNumber (17)|
|NextHop|Nexthop address|nexthop|From ExtendedGateway|IPV4_NEXT_HOP (15) BGP_IPV4_NEXT_HOP (18) IPV6_NEXT_HOP (62) BGP_IPV6_NEXT_HOP (63)|ipNextHopIPv4Address (15) bgpNextHopIPv4Address (18) ipNextHopIPv6Address (62) bgpNextHopIPv6Address (63)|
|NextHopAS|Nexthop AS number| |From ExtendedGateway| | |
|SrcNet|Source address mask|src_mask|From ExtendedRouter|SRC_MASK (9) IPV6_SRC_MASK (29)|sourceIPv4PrefixLength (9) sourceIPv6PrefixLength (29)|
|DstNet|Destination address mask|dst_mask|From ExtendedRouter|DST_MASK (13) IPV6_DST_MASK (30)|destinationIPv4PrefixLength (13) destinationIPv6PrefixLength (30)|
|HasMPLS|Indicates the presence of MPLS header||Included|||
|MPLSCount|Count of MPLS layers||Included|||
|MPLSxTTL|TTL of the MPLS label||Included|||
|MPLSxLabel|MPLS label||Included|||

