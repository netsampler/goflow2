package source

import (
	"context"
	"fmt"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/source/pcap"
	"github.com/netsampler/goflow2/v3/internal/reflow/source/socket"
)

type Source interface {
	InitEvents() ([]*event.Event, error)
	Start(context.Context, func(*event.Event) error) error
	Close() error
}

// New selects the source implementation from the source.network setting.
func New(cfg config.SourceConfig) (Source, error) {
	switch cfg.Network {
	case "udp", "unixgram":
		return socket.New(cfg)
	case "pcap_live":
		return pcap.New(cfg)
	default:
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
}
