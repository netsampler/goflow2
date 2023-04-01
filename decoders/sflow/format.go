package sflow

import (
	"fmt"
)

func (p *Packet) MarshalJSON() ([]byte, error) {
	return []byte("todo"), nil
}

func (p *Packet) String() string {
	return fmt.Sprintf("sFlow %d", p.Version)
}
