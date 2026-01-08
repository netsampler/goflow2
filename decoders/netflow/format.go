package netflow

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON encodes the packet without triggering MarshalText.
func (p *IPFIXPacket) MarshalJSON() ([]byte, error) {
	return json.Marshal(*p) // this is a trick to avoid having the JSON marshaller defaults to MarshalText
}

// MarshalJSON encodes the packet without triggering MarshalText.
func (p *NFv9Packet) MarshalJSON() ([]byte, error) {
	return json.Marshal(*p) // this is a trick to avoid having the JSON marshaller defaults to MarshalText
}

// MarshalText formats a concise summary of the packet.
func (p *IPFIXPacket) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("IPFIX count:%d seq:%d", len(p.FlowSets), p.SequenceNumber)), nil
}

// MarshalText formats a concise summary of the packet.
func (p *NFv9Packet) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("NetFlowV%d count:%d seq:%d", p.Version, p.Count, p.SequenceNumber)), nil
}
