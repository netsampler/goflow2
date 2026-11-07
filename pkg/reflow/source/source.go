package source

import (
	"context"
	"fmt"

	"github.com/netsampler/goflow2/v3/pkg/reflow/buildprofile"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/afpacket"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/bpf"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/conntrack"
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
	case "conntrack":
		// Linux platform profile. Read connection-tracking events over Netlink.
		return conntrack.New(cfg)
	case "udp", "unixgram":
		// UDP works across platforms. unixgram requires OS support for Unix
		// datagram sockets.
		return socket.New(cfg)
	case "af_packet":
		// Linux platform profile. Capture via a memory-mapped AF_PACKET ring.
		return afpacket.New(cfg)
	case "xdp":
		// Linux platform profile. Observe ingress packets at an XDP hook.
		return xdp.New(cfg)
	case "nflog":
		// Linux platform profile. Receive packet copies from firewall log rules.
		return nflog.New(cfg)
	case "psample":
		// Linux platform profile. Receive kernel or switch packet samples over Netlink.
		return psample.New(cfg)
	case "bpf":
		// macOS, FreeBSD, OpenBSD, and NetBSD platform profiles. Read native BPF devices.
		return bpf.New(cfg)
	case "pktap":
		// macOS platform profile. Capture through PKTAP with packet metadata.
		return pktap.New(cfg)
	case "pktmon":
		// Windows platform profile. Read the native Packet Monitor stream.
		return pktmon.New(cfg)
	case "dpdk":
		// Linux dpdk profile. Capture from DPDK receive queues.
		return dpdk.New(cfg)
	case "pfring", "pfring_zc":
		// Linux pfring profile. Zero-copy capture also requires ZC support
		// compiled in with a compatible SDK on amd64 or arm64.
		return pfring.New(cfg)
	case "ebpf":
		// Linux platform profile. Capture through an eBPF socket filter.
		return ebpf.New(cfg)
	case "pcap_live":
		// pcap profile. Use libpcap on Unix-like systems or Npcap on Windows.
		return pcap.New(cfg)
	case "stream":
		// Cross-platform. Read JSON, pcap, or pcapng from a file or stdin.
		return stream.New(cfg)
	default:
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
}
