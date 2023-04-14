package netflow

import (
	"fmt"
)

func (p *IPFIXPacket) MarshalJSON() ([]byte, error) {
	return []byte("todo"), nil
}

func (p *NFv9Packet) MarshalJSON() ([]byte, error) {
	return []byte("todo"), nil
}

func (p *IPFIXPacket) String() string {
	return fmt.Sprintf("IPFIX count:%d seq:%d", len(p.FlowSets), p.SequenceNumber)
}

func (p *NFv9Packet) String() string {
	return fmt.Sprintf("NetFlowV%d count:%d seq:%d", p.Version, p.Count, p.SequenceNumber)
}
