//go:build linux && cgo && reflow_pfring && !reflow_pcap && !reflow_dpdk && reflow_pfring_zc && (amd64 || arm64)

package pfring

/*
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <errno.h>
#include <string.h>
#include <pfring_zc.h>

typedef struct {
    pfring_zc_cluster *cluster;
    pfring_zc_queue *queue;
    pfring_zc_buffer_pool *pool;
    pfring_zc_pkt_buff *packet;
    int ipc;
} reflow_zc_handle;

static void reflow_zc_close(reflow_zc_handle *handle) {
    if (!handle) return;
    if (handle->ipc) {
        if (handle->packet) pfring_zc_release_packet_handle_to_pool(handle->pool, handle->packet);
        if (handle->queue) pfring_zc_ipc_detach_queue(handle->queue);
        if (handle->pool) pfring_zc_ipc_detach_buffer_pool(handle->pool);
    } else if (handle->cluster) {
        if (handle->packet) pfring_zc_release_packet_handle(handle->cluster, handle->packet);
        // Destroying our cluster releases its device queue and hugepages.
        pfring_zc_destroy_cluster(handle->cluster);
    }
    free(handle);
}

static reflow_zc_handle *reflow_zc_open(const char *device, uint32_t cluster_id,
        uint32_t queue_id, uint32_t pool_id, uint32_t snaplen, uint32_t buffers,
        int ipc, int promisc, char *error, size_t errorlen) {
    reflow_zc_handle *handle = calloc(1, sizeof(*handle));
    if (!handle) { snprintf(error, errorlen, "allocate PF_RING ZC handle"); return NULL; }
    handle->ipc = ipc;
    if (ipc) {
        pfring_zc_ipc_init(NULL);
        handle->pool = pfring_zc_ipc_attach_buffer_pool(cluster_id, pool_id);
        if (!handle->pool) goto fail;
        handle->packet = pfring_zc_get_packet_handle_from_pool(handle->pool);
        if (!handle->packet) goto fail;
        handle->queue = pfring_zc_ipc_attach_queue(cluster_id, queue_id, rx_only);
        if (!handle->queue) goto fail;
    } else {
        handle->cluster = pfring_zc_create_cluster(cluster_id, snaplen, 0, buffers, -1, NULL, 0);
        if (!handle->cluster) goto fail;
        uint32_t flags = PF_RING_ZC_DEVICE_SW_TIMESTAMP | PF_RING_ZC_DEVICE_NOT_REPROGRAM_RSS;
        if (!promisc) flags |= PF_RING_ZC_DEVICE_NOT_PROMISC;
        handle->queue = pfring_zc_open_device(handle->cluster, device, rx_only, flags);
        if (!handle->queue) goto fail;
        handle->packet = pfring_zc_get_packet_handle(handle->cluster);
        if (!handle->packet) goto fail;
    }
    return handle;
fail:
    snprintf(error, errorlen, "open PF_RING ZC %s: %s", ipc ? "IPC queue/pool" : "device", strerror(errno));
    reflow_zc_close(handle);
    return NULL;
}

static int reflow_zc_read(reflow_zc_handle *handle) {
    return pfring_zc_recv_pkt(handle->queue, &handle->packet, 0);
}
static void *reflow_zc_data(reflow_zc_handle *handle) {
    return pfring_zc_pkt_buff_data(handle->packet, handle->queue);
}
static uint32_t reflow_zc_length(reflow_zc_handle *handle) { return handle->packet->len; }
static uint64_t reflow_zc_timestamp(reflow_zc_handle *handle) {
    return (uint64_t)handle->packet->ts.tv_sec * 1000000000ULL + handle->packet->ts.tv_nsec;
}
static uint64_t reflow_zc_drops(reflow_zc_handle *handle) {
    pfring_zc_stat stats;
    if (pfring_zc_stats(handle->queue, &stats) < 0) return 0;
    return stats.drop;
}
*/
import "C"

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

type zcDriver struct {
	cfg    config.SourceConfig
	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func newZC(cfg config.SourceConfig) (*capture.Source, error) {
	if cfg.Capture.Direction != "ingress" {
		return nil, fmt.Errorf("pfring_zc captures RX traffic; capture.direction must be ingress")
	}
	if cfg.Capture.PFRing.Buffers == 0 {
		cfg.Capture.PFRing.Buffers = 8192
	}
	if cfg.Capture.SnapLen > 65535 {
		return nil, fmt.Errorf("PF_RING ZC snaplen must not exceed 65535")
	}
	return capture.New(cfg, &zcDriver{cfg: cfg, done: make(chan struct{})})
}

func (d *zcDriver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	select {
	case <-d.done:
		return nil
	default:
	}
	device := C.CString(d.cfg.Capture.Interface)
	defer C.free(unsafe.Pointer(device))
	ipc, promisc := C.int(0), C.int(1)
	if d.cfg.Capture.PFRing.ZCIPC {
		ipc = 1
	}
	if d.cfg.Capture.Promiscuous != nil && !*d.cfg.Capture.Promiscuous {
		promisc = 0
	}
	var errbuf [256]C.char
	handle := C.reflow_zc_open(device, C.uint32_t(d.cfg.Capture.PFRing.ZCClusterID), C.uint32_t(d.cfg.Capture.PFRing.ZCQueueID), C.uint32_t(d.cfg.Capture.PFRing.ZCPoolID), C.uint32_t(d.cfg.Capture.SnapLen), C.uint32_t(d.cfg.Capture.PFRing.Buffers), ipc, promisc, &errbuf[0], 256)
	if handle == nil {
		return fmt.Errorf("%s", C.GoString(&errbuf[0]))
	}
	defer C.reflow_zc_close(handle)
	ifIndex := 0
	if iface, err := net.InterfaceByName(d.cfg.Capture.Interface); err == nil {
		ifIndex = iface.Index
	}
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
		result := C.reflow_zc_read(handle)
		if result < 0 {
			return fmt.Errorf("read PF_RING ZC: status %d", result)
		}
		if result == 0 {
			runtime.Gosched()
			continue
		}
		length := int(C.reflow_zc_length(handle))
		captured := min(length, d.cfg.Capture.SnapLen)
		data := C.reflow_zc_data(handle)
		if data == nil && captured != 0 {
			return fmt.Errorf("PF_RING ZC returned an unavailable packet buffer")
		}
		if time.Since(statsAt) >= time.Second {
			drops = uint64(C.reflow_zc_drops(handle))
			statsAt = time.Now()
		}
		timestamp := time.Time{}
		if nanos := uint64(C.reflow_zc_timestamp(handle)); nanos != 0 {
			timestamp = time.Unix(0, int64(nanos)).UTC()
		}
		fields := map[string]any{"pfring_zc_cluster": d.cfg.Capture.PFRing.ZCClusterID}
		if d.cfg.Capture.PFRing.ZCIPC {
			fields["pfring_zc_queue"] = d.cfg.Capture.PFRing.ZCQueueID
		}
		frame := capture.Frame{Data: C.GoBytes(data, C.int(captured)), Timestamp: timestamp, LinkType: 1, Interface: d.cfg.Capture.Interface, InterfaceIndex: ifIndex, Direction: "ingress", CaptureLength: captured, WireLength: length, Drops: drops, Fields: fields}
		if err := emit(frame); err != nil {
			return err
		}
	}
}

func (d *zcDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		close(d.done)
		d.closed = true
	}
	return nil
}

func ZCAvailable() bool { return true }
