package sflow

type SampledHeader struct {
	Protocol       uint32 `json:"protocol"`
	FrameLength    uint32 `json:"frame-length"`
	Stripped       uint32 `json:"stripped"`
	OriginalLength uint32 `json:"original-length"`
	HeaderData     []byte `json:"header-data"`
}

type SampledEthernet struct {
	Length  uint32 `json:"length"`
	SrcMac  []byte `json:"src-mac"`
	DstMac  []byte `json:"dst-mac"`
	EthType uint32 `json:"eth-type"`
}

type SampledIPBase struct {
	Length   uint32 `json:"length"`
	Protocol uint32 `json:"protocol"`
	SrcIP    []byte `json:"src-ip"`
	DstIP    []byte `json:"dst-ip"`
	SrcPort  uint32 `json:"src-port"`
	DstPort  uint32 `json:"dst-port"`
	TcpFlags uint32 `json:"tcp-flags"`
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
	NextHopIPVersion uint32 `json:"next-hop-ip-version"`
	NextHop          []byte `json:"next-hop"`
	SrcMaskLen       uint32 `json:"src-mask-len"`
	DstMaskLen       uint32 `json:"dst-mask-len"`
}

type ExtendedGateway struct {
	NextHopIPVersion  uint32
	NextHop           []byte
	AS                uint32
	SrcAS             uint32
	SrcPeerAS         uint32
	ASDestinations    uint32
	ASPathType        uint32
	ASPathLength      uint32
	ASPath            []uint32
	CommunitiesLength uint32
	Communities       []uint32
	LocalPref         uint32
}

type IfCounters struct {
	IfIndex            uint32
	IfType             uint32
	IfSpeed            uint64
	IfDirection        uint32
	IfStatus           uint32
	IfInOctets         uint64
	IfInUcastPkts      uint32
	IfInMulticastPkts  uint32
	IfInBroadcastPkts  uint32
	IfInDiscards       uint32
	IfInErrors         uint32
	IfInUnknownProtos  uint32
	IfOutOctets        uint64
	IfOutUcastPkts     uint32
	IfOutMulticastPkts uint32
	IfOutBroadcastPkts uint32
	IfOutDiscards      uint32
	IfOutErrors        uint32
	IfPromiscuousMode  uint32
}

type EthernetCounters struct {
	Dot3StatsAlignmentErrors           uint32
	Dot3StatsFCSErrors                 uint32
	Dot3StatsSingleCollisionFrames     uint32
	Dot3StatsMultipleCollisionFrames   uint32
	Dot3StatsSQETestErrors             uint32
	Dot3StatsDeferredTransmissions     uint32
	Dot3StatsLateCollisions            uint32
	Dot3StatsExcessiveCollisions       uint32
	Dot3StatsInternalMacTransmitErrors uint32
	Dot3StatsCarrierSenseErrors        uint32
	Dot3StatsFrameTooLongs             uint32
	Dot3StatsInternalMacReceiveErrors  uint32
	Dot3StatsSymbolErrors              uint32
}

type RawRecord struct {
	Data []byte `json:"data"`
}
