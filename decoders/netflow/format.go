package netflow

import (
	"encoding/json"
	"fmt"
)

func (p *IPFIXPacket) MarshalJSON() ([]byte, error) {
	return json.Marshal(*p) // this is a trick to avoid having the JSON marshaller defaults to MarshalText
}

func (p *NFv9Packet) MarshalJSON() ([]byte, error) {
	return json.Marshal(*p) // this is a trick to avoid having the JSON marshaller defaults to MarshalText
}

func (p *IPFIXPacket) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("IPFIX count:%d seq:%d", len(p.FlowSets), p.SequenceNumber)), nil
}

func (p *NFv9Packet) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("NetFlowV%d count:%d seq:%d", p.Version, p.Count, p.SequenceNumber)), nil
}
