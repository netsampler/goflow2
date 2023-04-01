package netflowlegacy

type PacketNetFlowV5 struct {
	Version          uint16
	Count            uint16
	SysUptime        uint32
	UnixSecs         uint32
	UnixNSecs        uint32
	FlowSequence     uint32
	EngineType       uint8
	EngineId         uint8
	SamplingInterval uint16
	Records          []RecordsNetFlowV5
}

type RecordsNetFlowV5 struct {
	SrcAddr  uint32
	DstAddr  uint32
	NextHop  uint32
	Input    uint16
	Output   uint16
	DPkts    uint32
	DOctets  uint32
	First    uint32
	Last     uint32
	SrcPort  uint16
	DstPort  uint16
	Pad1     byte
	TCPFlags uint8
	Proto    uint8
	Tos      uint8
	SrcAS    uint16
	DstAS    uint16
	SrcMask  uint8
	DstMask  uint8
	Pad2     uint16
}
