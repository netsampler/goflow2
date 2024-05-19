package netflowlegacy

import (
	"fmt"
)

type PacketNetFlowV5 struct {
	Version          uint16             `json:"version"`
	Count            uint16             `json:"count"`
	SysUptime        uint32             `json:"sys-uptime"`
	UnixSecs         uint32             `json:"unix-secs"`
	UnixNSecs        uint32             `json:"unix-nsecs"`
	FlowSequence     uint32             `json:"flow-sequence"`
	EngineType       uint8              `json:"engine-type"`
	EngineId         uint8              `json:"engine-id"`
	SamplingInterval uint16             `json:"sampling-interval"`
	Records          []RecordsNetFlowV5 `json:"records"`
}

type RecordsNetFlowV5 struct {
	SrcAddr  IPAddress `json:"src-addr"`
	DstAddr  IPAddress `json:"dst-addr"`
	NextHop  IPAddress `json:"next-hop"`
	Input    uint16    `json:"input"`
	Output   uint16    `json:"output"`
	DPkts    uint32    `json:"dpkts"`
	DOctets  uint32    `json:"doctets"`
	First    uint32    `json:"first"`
	Last     uint32    `json:"last"`
	SrcPort  uint16    `json:"src-port"`
	DstPort  uint16    `json:"dst-port"`
	Pad1     byte      `json:"pad1"`
	TCPFlags uint8     `json:"tcp-flags"`
	Proto    uint8     `json:"proto"`
	Tos      uint8     `json:"tos"`
	SrcAS    uint16    `json:"src-as"`
	DstAS    uint16    `json:"dst-as"`
	SrcMask  uint8     `json:"src-mask"`
	DstMask  uint8     `json:"dst-mask"`
	Pad2     uint16    `json:"pad2"`
}

type IPAddress uint32 // purely for the formatting purpose

func (s *IPAddress) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"%d.%d.%d.%d\"", *s>>24, (*s>>16)&0xFF, (*s>>8)&0xFF, *s&0xFF)), nil
}
