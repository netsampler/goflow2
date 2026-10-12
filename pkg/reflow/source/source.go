package source

import (
	"context"
	"fmt"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/ebpf"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/pcap"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/socket"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/stream"
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
	case "ebpf":
		return ebpf.New(cfg)
	case "pcap_live":
		return pcap.New(cfg)
	case "stream":
		return stream.New(cfg)
	default:
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
}
