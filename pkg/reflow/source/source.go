package source

import (
	"context"
	"fmt"

	"github.com/netsampler/goflow2/v3/pkg/reflow/buildprofile"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/afpacket"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/bpf"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/dpdk"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/nflog"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/pfring"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/pktap"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/pktmon"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/psample"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/xdp"

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
	if config.IsCaptureNetwork(cfg.Network) {
		if !buildprofile.Supports(cfg.Network) {
			return nil, fmt.Errorf("capture input %s is unavailable in %s profile on this OS; compiled inputs: %v", cfg.Network, buildprofile.Name, buildprofile.Inputs())
		}
		if err := config.NormalizeCapture(&cfg); err != nil {
			return nil, err
		}
	}
	switch cfg.Network {
	case "udp", "unixgram":
		return socket.New(cfg)
	case "af_packet":
		return afpacket.New(cfg)
	case "xdp":
		return xdp.New(cfg)
	case "nflog":
		return nflog.New(cfg)
	case "psample":
		return psample.New(cfg)
	case "bpf":
		return bpf.New(cfg)
	case "pktap":
		return pktap.New(cfg)
	case "pktmon":
		return pktmon.New(cfg)
	case "dpdk":
		return dpdk.New(cfg)
	case "pfring", "pfring_zc":
		return pfring.New(cfg)
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
