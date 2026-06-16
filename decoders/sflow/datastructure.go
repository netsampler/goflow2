package sflow

import "github.com/netsampler/goflow2/v3/decoders/utils"

// SampledHeader holds raw sampled header metadata.
type SampledHeader struct {
	Protocol       uint32 `json:"protocol"`
	FrameLength    uint32 `json:"frame-length"`
	Stripped       uint32 `json:"stripped"`
	OriginalLength uint32 `json:"original-length"`
	HeaderData     []byte `json:"header-data"`
}

// SampledEthernet holds Ethernet header fields for sampled frames.
type SampledEthernet struct {
	Length  uint32           `json:"length"`
	SrcMac  utils.MacAddress `json:"src-mac"`
	DstMac  utils.MacAddress `json:"dst-mac"`
	EthType uint32           `json:"eth-type"`
}

// SampledIPBase contains shared IP header fields for sampled packets.
type SampledIPBase struct {
	Length   uint32          `json:"length"`
	Protocol uint32          `json:"protocol"`
	SrcIP    utils.IPAddress `json:"src-ip"`
	DstIP    utils.IPAddress `json:"dst-ip"`
	SrcPort  uint32          `json:"src-port"`
	DstPort  uint32          `json:"dst-port"`
	TcpFlags uint32          `json:"tcp-flags"`
}

// SampledIPv4 extends SampledIPBase with IPv4 fields.
type SampledIPv4 struct {
	SampledIPBase
	Tos uint32 `json:"tos"`
}

// SampledIPv6 extends SampledIPBase with IPv6 fields.
type SampledIPv6 struct {
	SampledIPBase
	Priority uint32 `json:"priority"`
}

// ExtendedSwitch carries VLAN and priority information.
type ExtendedSwitch struct {
	SrcVlan     uint32 `json:"src-vlan"`
	SrcPriority uint32 `json:"src-priority"`
	DstVlan     uint32 `json:"dst-vlan"`
	DstPriority uint32 `json:"dst-priority"`
}

// ExtendedRouter carries next-hop and mask metadata.
type ExtendedRouter struct {
	NextHopIPVersion uint32          `json:"next-hop-ip-version"`
	NextHop          utils.IPAddress `json:"next-hop"`
	SrcMaskLen       uint32          `json:"src-mask-len"`
	DstMaskLen       uint32          `json:"dst-mask-len"`
}

// ASPathSegment represents a segment in an AS path.
type ASPathSegment struct {
	Type uint32   `json:"type"`
	Path []uint32 `json:"path"`
}

// ExtendedGateway carries BGP gateway attributes and AS paths.
type ExtendedGateway struct {
	NextHopIPVersion  uint32          `json:"next-hop-ip-version"`
	NextHop           utils.IPAddress `json:"next-hop"`
	AS                uint32          `json:"as"`
	SrcAS             uint32          `json:"src-as"`
	SrcPeerAS         uint32          `json:"src-peer-as"`
	ASDestinations    uint32          `json:"as-destinations"`
	ASPathType        uint32          `json:"as-path-type"`
	ASPathLength      uint32          `json:"as-path-length"`
	ASPath            []uint32        `json:"as-path"`
	DstASPath         []ASPathSegment `json:"dst-as-path"`
	CommunitiesLength uint32          `json:"communities-length"`
	Communities       []uint32        `json:"communities"`
	LocalPref         uint32          `json:"local-pref"`
}

// ExtendedMPLS carries MPLS next-hop and label stack metadata.
type ExtendedMPLS struct {
	NextHopIPVersion uint32          `json:"next-hop-ip-version"`
	NextHop          utils.IPAddress `json:"next-hop"`
	InLabelStack     []uint32        `json:"in-label-stack"`
	OutLabelStack    []uint32        `json:"out-label-stack"`
}

// ExtendedNAT carries translated source and destination addresses.
type ExtendedNAT struct {
	SrcAddressIPVersion uint32          `json:"src-address-ip-version"`
	SrcAddress          utils.IPAddress `json:"src-address"`
	DstAddressIPVersion uint32          `json:"dst-address-ip-version"`
	DstAddress          utils.IPAddress `json:"dst-address"`
}

// ExtendedMPLSTunnel carries MPLS tunnel metadata.
type ExtendedMPLSTunnel struct {
	TunnelLSPName string `json:"tunnel-lsp-name"`
	TunnelID      uint32 `json:"tunnel-id"`
	TunnelCOS     uint32 `json:"tunnel-cos"`
}

// ExtendedMPLSVC carries MPLS virtual circuit metadata.
type ExtendedMPLSVC struct {
	VCInstanceName string `json:"vc-instance-name"`
	VLLVCID        uint32 `json:"vll-vc-id"`
	VCLabelCOS     uint32 `json:"vc-label-cos"`
}

// ExtendedMPLSFTN carries MPLS FTN metadata.
type ExtendedMPLSFTN struct {
	MPLSFTNDescr string `json:"mpls-ftn-descr"`
	MPLSFTNMask  uint32 `json:"mpls-ftn-mask"`
}

// ExtendedMPLSLDPFEC carries MPLS LDP FEC metadata.
type ExtendedMPLSLDPFEC struct {
	MPLSFecAddrPrefixLength uint32 `json:"mpls-fec-addr-prefix-length"`
}

// EgressQueue reports a queue identifier for drop records.
type EgressQueue struct {
	Queue uint32 `json:"queue"`
}

// ExtendedACL describes an ACL match.
type ExtendedACL struct {
	Number    uint32 `json:"number"`
	Name      string `json:"name"`
	Direction uint32 `json:"direction"` // 0:unknown, 1:ingress, 2:egress
}

// ExtendedFunction identifies a forwarding function.
type ExtendedFunction struct {
	Symbol string `json:"symbol"`
}

// IfCounters stores interface counter statistics.
type IfCounters struct {
	IfIndex            uint32 `json:"if-index"`
	IfType             uint32 `json:"if-type"`
	IfSpeed            uint64 `json:"if-speed"`
	IfDirection        uint32 `json:"if-direction"`
	IfStatus           uint32 `json:"if-status"`
	IfInOctets         uint64 `json:"if-in-octets"`
	IfInUcastPkts      uint32 `json:"if-in-ucast-pkts"`
	IfInMulticastPkts  uint32 `json:"if-in-multicast-pkts"`
	IfInBroadcastPkts  uint32 `json:"if-in-broadcast-pkts"`
	IfInDiscards       uint32 `json:"if-in-discards"`
	IfInErrors         uint32 `json:"if-in-errors"`
	IfInUnknownProtos  uint32 `json:"if-in-unknown-protos"`
	IfOutOctets        uint64 `json:"if-out-octets"`
	IfOutUcastPkts     uint32 `json:"if-out-ucast-pkts"`
	IfOutMulticastPkts uint32 `json:"if-out-multicast-pkts"`
	IfOutBroadcastPkts uint32 `json:"if-out-broadcast-pkts"`
	IfOutDiscards      uint32 `json:"if-out-discards"`
	IfOutErrors        uint32 `json:"if-out-errors"`
	IfPromiscuousMode  uint32 `json:"if-promiscuous-mode"`
}

// EthernetCounters stores Ethernet-specific counter statistics.
type EthernetCounters struct {
	Dot3StatsAlignmentErrors           uint32 `json:"dot3-stats-aligment-errors"`
	Dot3StatsFCSErrors                 uint32 `json:"dot3-stats-fcse-errors"`
	Dot3StatsSingleCollisionFrames     uint32 `json:"dot3-stats-single-collision-frames"`
	Dot3StatsMultipleCollisionFrames   uint32 `json:"dot3-stats-multiple-collision-frames"`
	Dot3StatsSQETestErrors             uint32 `json:"dot3-stats-seq-test-errors"`
	Dot3StatsDeferredTransmissions     uint32 `json:"dot3-stats-deferred-transmissions"`
	Dot3StatsLateCollisions            uint32 `json:"dot3-stats-late-collisions"`
	Dot3StatsExcessiveCollisions       uint32 `json:"dot3-stats-excessive-collisions"`
	Dot3StatsInternalMacTransmitErrors uint32 `json:"dot3-stats-internal-mac-transmit-errors"`
	Dot3StatsCarrierSenseErrors        uint32 `json:"dot3-stats-carrier-sense-errors"`
	Dot3StatsFrameTooLongs             uint32 `json:"dot3-stats-frame-too-longs"`
	Dot3StatsInternalMacReceiveErrors  uint32 `json:"dot3-stats-internal-mac-receive-errors"`
	Dot3StatsSymbolErrors              uint32 `json:"dot3-stats-symbol-errors"`
}

// RawRecord stores unparsed record bytes.
type RawRecord struct {
	Data []byte `json:"data"`
}

// RawSample stores unparsed sample bytes for unknown standard or enterprise samples.
type RawSample struct {
	Header SampleHeader `json:"header"`
	Data   []byte       `json:"data"`
}
