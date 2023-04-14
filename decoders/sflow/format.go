package sflow

import (
	"encoding/json"
	"fmt"
)

func (p *Packet) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.Samples)
	return []byte("todo"), nil
}

func (p *Packet) String() string {
	return fmt.Sprintf("sFlow %d", p.Version)
}
