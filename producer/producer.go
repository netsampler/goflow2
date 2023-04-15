package producer

import (
	"net/netip"
	"time"
)

// Interface of the messages
type ProducerMessage interface{}

type ProducerInterface interface {
	// Converts a message into a list of flow samples
	Produce(msg interface{}, args *ProduceArgs) ([]ProducerMessage, error)
	// Indicates to the producer the messages returned were processed
	Commit([]ProducerMessage)
	Close()
}

type ProduceArgs struct {
	Src            netip.AddrPort
	Dst            netip.AddrPort
	SamplerAddress netip.Addr
	TimeReceived   time.Time
}
