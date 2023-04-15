package sflow

import (
	"encoding/json"
	"fmt"
	"net/netip"
)

func (p *Packet) MarshalJSON() ([]byte, error) {
	return json.Marshal(*p) // this is a trick to avoid having the JSON marshaller defaults to MarshalText
}

func (p *Packet) MarshalText() ([]byte, error) {
	agentIP, _ := netip.AddrFromSlice(p.AgentIP)
	return []byte(fmt.Sprintf("sFlow%d agent:%s seq:%d count:%d", p.Version, agentIP.String(), p.SequenceNumber, p.SamplesCount)), nil
}
