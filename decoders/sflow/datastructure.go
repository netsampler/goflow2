package sflow

import (
	"fmt"
	"net"
)

type SampledHeader struct {
	Protocol       uint32 `json:"protocol"`
	FrameLength    uint32 `json:"frame-length"`
	Stripped       uint32 `json:"stripped"`
	OriginalLength uint32 `json:"original-length"`
	HeaderData     []byte `json:"header-data"`
}

type SampledEthernet struct {
	Length  uint32     `json:"length"`
	SrcMac  MacAddress `json:"src-mac"`
	DstMac  MacAddress `json:"dst-mac"`
	EthType uint32     `json:"eth-type"`
}

type SampledIPBase struct {
	Length   uint32    `json:"length"`
	Protocol uint32    `json:"protocol"`
	SrcIP    IPAddress `json:"src-ip"`
	DstIP    IPAddress `json:"dst-ip"`
	SrcPort  uint32    `json:"src-port"`
	DstPort  uint32    `json:"dst-port"`
	TcpFlags uint32    `json:"tcp-flags"`
}

type MacAddress []byte // purely for the formatting purpose

func (s *MacAddress) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%s\"", net.HardwareAddr([]byte(*s)).String())), nil
}

type SampledIPv4 struct {
	SampledIPBase
	Tos uint32 `json:"tos"`
}

type SampledIPv6 struct {
	SampledIPBase
	Priority uint32 `json:"priority"`
}

type ExtendedSwitch struct {
	SrcVlan     uint32 `json:"src-vlan"`
	SrcPriority uint32 `json:"src-priority"`
	DstVlan     uint32 `json:"dst-vlan"`
	DstPriority uint32 `json:"dst-priority"`
}

type ExtendedRouter struct {
	NextHopIPVersion uint32    `json:"next-hop-ip-version"`
	NextHop          IPAddress `json:"next-hop"`
	SrcMaskLen       uint32    `json:"src-mask-len"`
	DstMaskLen       uint32    `json:"dst-mask-len"`
}

type ExtendedGateway struct {
	NextHopIPVersion  uint32    `json:"next-hop-ip-version"`
	NextHop           IPAddress `json:"next-hop"`
	AS                uint32    `json:"as"`
	SrcAS             uint32    `json:"src-as"`
	SrcPeerAS         uint32    `json:"src-peer-as"`
	ASDestinations    uint32    `json:"as-destinations"`
	ASPathType        uint32    `json:"as-path-type"`
	ASPathLength      uint32    `json:"as-path-length"`
	ASPath            []uint32  `json:"as-path"`
	CommunitiesLength uint32    `json:"communities-length"`
	Communities       []uint32  `json:"communities"`
	LocalPref         uint32    `json:"local-pref"`
}

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

type RawRecord struct {
	Data []byte `json:"data"`
}
