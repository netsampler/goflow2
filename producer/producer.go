// Package producer converts decoded packets into output messages.
package producer

import (
	"net/netip"
	"time"
)

// ProducerMessage is the generic type returned by producers.
type ProducerMessage interface{}

// DecodedPacket represents a decoded flow packet ready for production.
type DecodedPacket interface {
	DecodedPacket()
}

// ProducerInterface converts decoded packets into producer messages.
type ProducerInterface interface {
	// Converts a message into a list of flow samples
	Produce(msg DecodedPacket, args *ProduceArgs) ([]ProducerMessage, error)
	// Indicates to the producer the messages returned were processed
	Commit([]ProducerMessage)
	Close()
}

// ProduceArgs captures metadata about the received packet.
type ProduceArgs struct {
	Src            netip.AddrPort
	Dst            netip.AddrPort
	SamplerAddress netip.Addr
	TimeReceived   time.Time
}
