package encode

import (
	"fmt"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type Encoder interface {
	Encode(evt *event.Event) ([][]byte, error)
	Flush() ([][]byte, error)
}

// New builds the configured encoder. Each encoder worker gets its own instance.
func New(cfg config.EncoderConfig) (Encoder, error) {
	switch cfg.Type {
	case "", "json":
		return NewJSONEncoder(cfg), nil
	case "protobuf":
		return NewProtobufEncoder(cfg)
	case "sflow":
		return NewSFlowEncoder(cfg), nil
	case "ipfix":
		return NewIPFIXEncoder(cfg), nil
	case "netflowv9":
		return NewNFv9Encoder(cfg), nil
	case "netflowv5":
		return NewNFv5Encoder(cfg), nil
	case "pcap":
		return NewPcapEncoder(cfg)
	case "pcapng":
		return NewPcapNGEncoder(cfg)
	default:
		return nil, fmt.Errorf("unsupported encoder.type %q", cfg.Type)
	}
}
