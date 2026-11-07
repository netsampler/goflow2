//go:build linux && cgo && reflow_pfring && !reflow_pcap && !reflow_dpdk

package pfring

/*
#cgo LDFLAGS: -lpfring -lpthread
#include <stdint.h>
#include <stdlib.h>
#include <pfring.h>

typedef struct {
    const unsigned char *data;
    uint32_t captured;
    uint32_t length;
    uint32_t ifindex;
    uint64_t timestamp;
    uint8_t rx;
} reflow_ring_packet;

static int reflow_ring_read(pfring *ring, reflow_ring_packet *packet) {
    unsigned char *data = NULL;
    struct pfring_pkthdr hdr;
    memset(&hdr, 0, sizeof(hdr));
    int result = pfring_recv(ring, &data, 0, &hdr, 0);
    if (result == 1) {
        packet->data = data;
        packet->captured = hdr.caplen;
        packet->length = hdr.len;
        packet->ifindex = hdr.extended_hdr.if_index;
        packet->rx = hdr.extended_hdr.rx_direction;
        packet->timestamp = hdr.extended_hdr.timestamp_ns;
        if (!packet->timestamp)
            packet->timestamp = (uint64_t)hdr.ts.tv_sec * 1000000000ULL + (uint64_t)hdr.ts.tv_usec * 1000ULL;
    }
    return result;
}

static uint64_t reflow_ring_drops(pfring *ring) {
    pfring_stat stats;
    if (pfring_stats(ring, &stats) < 0) return 0;
    return stats.drop;
}
*/
import "C"

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
	"unsafe"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

type driver struct {
	cfg    config.SourceConfig
	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func SDKVersion() string {
	var version C.u_int32_t
	C.pfring_version_noring(&version)
	return fmt.Sprintf("%d.%d.%d", uint32(version)>>16, uint32(version)>>8&255, uint32(version)&255)
}

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	if cfg.Network == "pfring_zc" {
		if cfg.Address == "" && cfg.Capture.PFRing.ZCIPC {
			cfg.Address = fmt.Sprintf("ipc:%d:%d", cfg.Capture.PFRing.ZCClusterID, cfg.Capture.PFRing.ZCQueueID)
		}
		return newZC(cfg)
	}
	if cfg.Network != "pfring" {
		return nil, fmt.Errorf("unsupported PF_RING input %q", cfg.Network)
	}
	switch cfg.Capture.PFRing.ClusterType {
	case "", "flow", "round_robin":
	default:
		return nil, fmt.Errorf("capture.pfring.cluster_type must be flow or round_robin")
	}
	return capture.New(cfg, &driver{cfg: cfg, done: make(chan struct{})})
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	select {
	case <-d.done:
		return nil
	default:
	}
	device := C.CString(d.cfg.Capture.Interface)
	defer C.free(unsafe.Pointer(device))
	flags := C.u_int32_t(C.PF_RING_LONG_HEADER | C.PF_RING_TIMESTAMP)
	if d.cfg.Capture.Promiscuous == nil || *d.cfg.Capture.Promiscuous {
		flags |= C.PF_RING_PROMISC
	}
	ring := C.pfring_open(device, C.u_int32_t(d.cfg.Capture.SnapLen), flags)
	if ring == nil {
		return fmt.Errorf("open PF_RING device %q: check PF_RING module, device, and permissions", d.cfg.Capture.Interface)
	}
	defer C.pfring_close(ring)
	if result := C.pfring_set_socket_mode(ring, C.recv_only_mode); result < 0 {
		return fmt.Errorf("set PF_RING receive mode: status %d", result)
	}
	direction := C.packet_direction(C.rx_and_tx_direction)
	switch d.cfg.Capture.Direction {
	case "ingress":
		direction = C.rx_only_direction
	case "egress":
		direction = C.tx_only_direction
	}
	if result := C.pfring_set_direction(ring, direction); result < 0 {
		return fmt.Errorf("set PF_RING direction: status %d", result)
	}
	if d.cfg.Capture.PFRing.ClusterID != 0 {
		clusterType := C.cluster_type(C.cluster_per_flow)
		if d.cfg.Capture.PFRing.ClusterType == "round_robin" {
			clusterType = C.cluster_round_robin
		}
		if result := C.pfring_set_cluster(ring, C.u_int(d.cfg.Capture.PFRing.ClusterID), clusterType); result < 0 {
			return fmt.Errorf("set PF_RING cluster: status %d", result)
		}
	}
	if result := C.pfring_enable_ring(ring); result < 0 {
		return fmt.Errorf("enable PF_RING capture: status %d", result)
	}
	ifIndex := 0
	if iface, err := net.InterfaceByName(d.cfg.Capture.Interface); err == nil {
		ifIndex = iface.Index
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	statsAt := time.Time{}
	var drops uint64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-d.done:
			return nil
		default:
		}
		var packet C.reflow_ring_packet
		result := C.reflow_ring_read(ring, &packet)
		if result < 0 {
			return fmt.Errorf("read PF_RING capture: status %d", result)
		}
		if result == 0 {
			select {
			case <-ctx.Done():
				return nil
			case <-d.done:
				return nil
			case <-ticker.C:
			}
			continue
		}
		if time.Since(statsAt) >= time.Second {
			drops = uint64(C.reflow_ring_drops(ring))
			statsAt = time.Now()
		}
		index := ifIndex
		if packet.ifindex != 0 && uint32(packet.ifindex) != 0xffffffff {
			index = int(packet.ifindex)
		}
		packetDirection := "egress"
		if packet.rx != 0 {
			packetDirection = "ingress"
		}
		if d.cfg.Capture.Direction != "both" {
			packetDirection = d.cfg.Capture.Direction
		}
		timestamp := time.Time{}
		if packet.timestamp != 0 {
			timestamp = time.Unix(0, int64(packet.timestamp)).UTC()
		}
		frame := capture.Frame{Data: C.GoBytes(unsafe.Pointer(packet.data), C.int(packet.captured)), Timestamp: timestamp, LinkType: 1, Interface: d.cfg.Capture.Interface, InterfaceIndex: index, Direction: packetDirection, CaptureLength: int(packet.captured), WireLength: int(packet.length), Drops: drops}
		if d.cfg.Capture.Interface == "any" {
			frame.Interface = ""
		}
		if err := emit(frame); err != nil {
			return err
		}
	}
}

// Close wakes the nonblocking polling loop. Its owning goroutine closes C handles.
func (d *driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		close(d.done)
		d.closed = true
	}
	return nil
}
