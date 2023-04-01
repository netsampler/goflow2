package sflow

import (
	"fmt"
)

func (p *Packet) String() string {
	return fmt.Sprintf("sFlow %d", p.Version)
}
