package sflow

import (
	//"encoding/json"
	"fmt"
	"net/netip"
)

/*
func (p *Packet) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.Samples)
	// todo: improve
	return []byte("todo"), nil
}*/

func (p *Packet) String() string {
	agentIP, _ := netip.AddrFromSlice(p.AgentIP)
	return fmt.Sprintf("sFlow%d agent:%s seq:%d count:%d", p.Version, agentIP.String(), p.SequenceNumber, p.SamplesCount)
}
