//go:build linux && cgo && reflow_dpdk && !reflow_pcap && !reflow_pfring

package dpdk

/*
#cgo pkg-config: libdpdk
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <errno.h>
#include <stdint.h>
#include <rte_eal.h>
#include <rte_ethdev.h>
#include <rte_mbuf.h>
#include <rte_lcore.h>
#include <rte_errno.h>
#include <rte_version.h>

#define REFLOW_DPDK_BURST 64
static const char *reflow_dpdk_error(void) { return rte_strerror(rte_errno); }

typedef struct {
    uint16_t port;
    uint16_t queues;
    uint32_t snaplen;
    struct rte_mempool *pool;
    unsigned char *scratch;
    struct rte_mbuf *packets[REFLOW_DPDK_BURST];
    const void *data[REFLOW_DPDK_BURST];
    uint32_t captured[REFLOW_DPDK_BURST];
    uint32_t lengths[REFLOW_DPDK_BURST];
} reflow_dpdk_port;

static int reflow_dpdk_resolve(const char *name, uint16_t *port) {
    char *end;
    unsigned long number = strtoul(name, &end, 10);
    if (*name && *end == '\0' && number <= UINT16_MAX) {
        *port = (uint16_t)number;
        return rte_eth_dev_is_valid_port(*port) ? 0 : -1;
    }
    return rte_eth_dev_get_port_by_name(name, port);
}

static reflow_dpdk_port *reflow_dpdk_open(uint16_t port, uint16_t queues,
        uint16_t descriptors, uint32_t mbufs, uint32_t snaplen, int promisc,
        char *error, size_t errorlen) {
    reflow_dpdk_port *handle = calloc(1, sizeof(*handle));
    if (!handle) { snprintf(error, errorlen, "allocate capture handle"); return NULL; }
    handle->port = port; handle->queues = queues; handle->snaplen = snaplen;
    struct rte_eth_dev_info info;
    int rc = rte_eth_dev_info_get(port, &info);
    if (rc < 0) goto fail;
    if (queues > info.max_rx_queues) { rc = -EINVAL; goto fail; }
    struct rte_eth_conf configuration;
    memset(&configuration, 0, sizeof(configuration));
    if (info.rx_offload_capa & RTE_ETH_RX_OFFLOAD_SCATTER)
        configuration.rxmode.offloads |= RTE_ETH_RX_OFFLOAD_SCATTER;
    // Enable RSS only when the PMD supports the requested hash functions.
    if (queues > 1) {
        uint64_t rss = info.flow_type_rss_offloads & RTE_ETH_RSS_IP;
        if (rss) {
            configuration.rxmode.mq_mode = RTE_ETH_MQ_RX_RSS;
            configuration.rx_adv_conf.rss_conf.rss_hf = rss;
        }
    }
    rc = rte_eth_dev_configure(port, queues, 0, &configuration);
    if (rc < 0) goto fail;
    char poolname[64];
    snprintf(poolname, sizeof(poolname), "reflow_rx_%u", port);
    int socket = rte_eth_dev_socket_id(port);
    if (socket < 0) socket = SOCKET_ID_ANY;
    // Standard mbufs allow the PMD to deliver multisegment/jumbo packets;
    // rte_pktmbuf_read below assembles a selected capture into scratch space.
    handle->pool = rte_pktmbuf_pool_create(poolname, mbufs, 0, 0,
        RTE_MBUF_DEFAULT_BUF_SIZE, socket);
    if (!handle->pool) { rc = -rte_errno; goto fail; }
    uint16_t txdescriptors = descriptors;
    rc = rte_eth_dev_adjust_nb_rx_tx_desc(port, &descriptors, &txdescriptors);
    if (rc < 0) goto fail;
    for (uint16_t queue = 0; queue < queues; queue++) {
        struct rte_eth_rxconf rxconf = info.default_rxconf;
        rxconf.offloads = configuration.rxmode.offloads;
        rc = rte_eth_rx_queue_setup(port, queue, descriptors, socket,
            &rxconf, handle->pool);
        if (rc < 0) goto fail;
    }
    handle->scratch = malloc((size_t)snaplen * REFLOW_DPDK_BURST);
    if (!handle->scratch) { rc = -ENOMEM; goto fail; }
    rc = rte_eth_dev_start(port);
    if (rc < 0) goto fail;
    if (promisc) {
        rc = rte_eth_promiscuous_enable(port);
        if (rc < 0) goto fail;
    }
    return handle;
fail:
    snprintf(error, errorlen, "configure DPDK port %u: %s", port, rte_strerror(-rc));
    rte_eth_dev_stop(port);
    rte_eth_dev_close(port);
    if (handle->pool) rte_mempool_free(handle->pool);
    free(handle->scratch);
    free(handle);
    return NULL;
}

static uint16_t reflow_dpdk_rx(reflow_dpdk_port *handle, uint16_t queue) {
    uint16_t count = rte_eth_rx_burst(handle->port, queue, handle->packets, REFLOW_DPDK_BURST);
    for (uint16_t i = 0; i < count; i++) {
        uint32_t length = rte_pktmbuf_pkt_len(handle->packets[i]);
        uint32_t capture = length < handle->snaplen ? length : handle->snaplen;
        handle->lengths[i] = length;
        handle->captured[i] = capture;
        handle->data[i] = rte_pktmbuf_read(handle->packets[i], 0, capture,
            handle->scratch + (size_t)i * handle->snaplen);
    }
    return count;
}

static void reflow_dpdk_release(reflow_dpdk_port *handle, uint16_t count) {
    for (uint16_t i = 0; i < count; i++) rte_pktmbuf_free(handle->packets[i]);
}

static void reflow_dpdk_close(reflow_dpdk_port *handle) {
    rte_eth_dev_stop(handle->port);
    rte_eth_dev_close(handle->port);
    rte_mempool_free(handle->pool);
    free(handle->scratch);
    free(handle);
}
*/
import "C"

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

var eal struct {
	sync.Mutex
	initialized     bool
	stopped         bool
	args            []string
	argv            unsafe.Pointer
	argumentStrings []unsafe.Pointer
	ports           map[uint16]bool
	users           int
}

type driver struct {
	cfg    config.SourceConfig
	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func SDKVersion() string { return C.GoString(C.rte_version()) }

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	if cfg.Capture.Direction != "" && cfg.Capture.Direction != "ingress" {
		return nil, fmt.Errorf("dpdk captures RX traffic; capture.direction must be ingress")
	}
	if cfg.Capture.DPDK.Port == "" {
		return nil, fmt.Errorf("capture.dpdk.port is required")
	}
	if cfg.Address == "" {
		cfg.Address = cfg.Capture.DPDK.Port
	}
	queues := cfg.Capture.DPDK.Queues
	if len(queues) == 0 {
		queues = []uint16{0}
		cfg.Capture.DPDK.Queues = queues
	}
	used := make(map[uint16]bool)
	for _, queue := range queues {
		if queue == 65535 || used[queue] {
			return nil, fmt.Errorf("DPDK RX queues must be distinct and below 65535")
		}
		used[queue] = true
	}
	for queue := 0; queue < len(queues); queue++ {
		if !used[uint16(queue)] {
			return nil, fmt.Errorf("DPDK source owns its port: queues must be a contiguous set starting at 0")
		}
	}
	if cfg.Capture.DPDK.Descriptors == 0 {
		cfg.Capture.DPDK.Descriptors = 1024
	}
	if cfg.Capture.DPDK.Descriptors < 1 || cfg.Capture.DPDK.Descriptors > 65535 {
		return nil, fmt.Errorf("DPDK descriptors must be between 1 and 65535")
	}
	if cfg.Capture.DPDK.Mbufs == 0 {
		cfg.Capture.DPDK.Mbufs = 8191
	}
	if cfg.Capture.DPDK.Mbufs < 1 {
		return nil, fmt.Errorf("DPDK mbufs must be positive")
	}
	return capture.New(cfg, &driver{cfg: cfg, done: make(chan struct{})})
}

// acquire initializes the process-wide EAL on a locked thread. DPDK ports are
// exclusively owned by one source, which may consume multiple RX queues.
func (d *driver) acquire() (uint16, bool, error) {
	eal.Lock()
	defer eal.Unlock()
	args := d.cfg.Capture.DPDK.EALArgs
	if eal.stopped {
		return 0, false, fmt.Errorf("DPDK EAL has already shut down; create all DPDK sources in one run")
	}
	first := !eal.initialized
	if first {
		stringsArgs := append([]string{"reflow"}, args...)
		argv := C.calloc(C.size_t(len(stringsArgs)+1), C.size_t(unsafe.Sizeof(uintptr(0))))
		if argv == nil {
			return 0, false, fmt.Errorf("allocate DPDK EAL arguments")
		}
		values := unsafe.Slice((**C.char)(argv), len(stringsArgs))
		allocated := make([]unsafe.Pointer, len(stringsArgs))
		for i, value := range stringsArgs {
			values[i] = C.CString(value)
			allocated[i] = unsafe.Pointer(values[i])
		}
		// EAL retains argv-derived storage; keep the allocated argument strings
		// until Cleanup rather than exposing Go memory to C.
		if C.rte_eal_init(C.int(len(stringsArgs)), (**C.char)(argv)) < 0 {
			for _, value := range allocated {
				C.free(value)
			}
			C.free(argv)
			return 0, false, fmt.Errorf("initialize DPDK EAL (args %s): %s", strings.Join(args, " "), C.GoString(C.reflow_dpdk_error()))
		}
		eal.argv = argv
		eal.argumentStrings = allocated
		eal.args = append([]string(nil), args...)
		eal.initialized = true
		eal.ports = make(map[uint16]bool)
	} else if !reflect.DeepEqual(eal.args, append([]string(nil), args...)) {
		return 0, false, fmt.Errorf("all DPDK sources must use identical capture.dpdk.eal_args")
	}
	name := C.CString(d.cfg.Capture.DPDK.Port)
	defer C.free(unsafe.Pointer(name))
	var port C.uint16_t
	if C.reflow_dpdk_resolve(name, &port) != 0 {
		return 0, first, fmt.Errorf("DPDK port %q is unavailable", d.cfg.Capture.DPDK.Port)
	}
	if eal.ports[uint16(port)] {
		return 0, first, fmt.Errorf("DPDK port %d is already owned by another source; configure all its RX queues on one source", port)
	}
	eal.ports[uint16(port)] = true
	eal.users++
	return uint16(port), first, nil
}

func release(port uint16) {
	eal.Lock()
	defer eal.Unlock()
	delete(eal.ports, port)
	eal.users--
	// EAL remains initialized until process exit. Other sources may still be
	// starting, so a temporary zero-user interval must not tear it down.
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// EAL may pin its initializing thread; restore the Go worker's original
	// affinity before returning it to the runtime scheduler.
	var affinity unix.CPUSet
	if unix.SchedGetaffinity(0, &affinity) == nil {
		defer func() { _ = unix.SchedSetaffinity(0, &affinity) }()
	}
	select {
	case <-d.done:
		return nil
	default:
	}
	port, first, err := d.acquire()
	if err != nil {
		return err
	}
	defer release(port)
	if !first {
		if C.rte_thread_register() < 0 {
			return fmt.Errorf("register DPDK receive thread")
		}
		defer C.rte_thread_unregister()
	}
	queueCount := uint16(1)
	for _, queue := range d.cfg.Capture.DPDK.Queues {
		if queue >= queueCount {
			queueCount = queue + 1
		}
	}
	var errbuf [256]C.char
	promisc := C.int(1)
	if d.cfg.Capture.Promiscuous != nil && !*d.cfg.Capture.Promiscuous {
		promisc = 0
	}
	handle := C.reflow_dpdk_open(C.uint16_t(port), C.uint16_t(queueCount), C.uint16_t(d.cfg.Capture.DPDK.Descriptors), C.uint32_t(d.cfg.Capture.DPDK.Mbufs), C.uint32_t(d.cfg.Capture.SnapLen), promisc, &errbuf[0], 256)
	if handle == nil {
		return fmt.Errorf("%s", C.GoString(&errbuf[0]))
	}
	defer C.reflow_dpdk_close(handle)
	statsAt := time.Time{}
	fields := map[string]any{}
	var drops uint64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-d.done:
			return nil
		default:
		}
		if time.Since(statsAt) >= time.Second {
			var stats C.struct_rte_eth_stats
			if C.rte_eth_stats_get(C.uint16_t(port), &stats) == 0 {
				drops = uint64(stats.imissed) + uint64(stats.rx_nombuf)
				fields = map[string]any{"dpdk_rx_missed": uint64(stats.imissed), "dpdk_rx_no_mbuf": uint64(stats.rx_nombuf), "dpdk_rx_errors": uint64(stats.ierrors)}
			}
			statsAt = time.Now()
		}
		received := false
		for _, queue := range d.cfg.Capture.DPDK.Queues {
			count := C.reflow_dpdk_rx(handle, C.uint16_t(queue))
			if count == 0 {
				continue
			}
			received = true
			var emitErr error
			for i := 0; i < int(count); i++ {
				if handle.data[i] == nil {
					emitErr = fmt.Errorf("read DPDK packet segments on port %d queue %d", port, queue)
					break
				}
				packetFields := make(map[string]any, len(fields)+2)
				for key, value := range fields {
					packetFields[key] = value
				}
				packetFields["dpdk_port"] = uint32(port)
				packetFields["dpdk_queue"] = uint32(queue)
				frame := capture.Frame{Data: C.GoBytes(handle.data[i], C.int(handle.captured[i])), Timestamp: time.Now().UTC(), LinkType: 1, Interface: "dpdk:" + strconv.Itoa(int(port)), Direction: "ingress", CaptureLength: int(handle.captured[i]), WireLength: int(handle.lengths[i]), Fields: packetFields, Drops: drops}
				if emitErr = emit(frame); emitErr != nil {
					break
				}
			}
			C.reflow_dpdk_release(handle, count)
			if emitErr != nil {
				return emitErr
			}
		}
		if !received {
			runtime.Gosched()
		}
	}
}

// Close signals the polling owner; C handles are released on that same thread.
func (d *driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		close(d.done)
		d.closed = true
	}
	return nil
}

// Cleanup releases process-wide EAL resources after every capture worker has
// exited. EAL cannot be initialized again in the same process after cleanup.
func Cleanup() error {
	eal.Lock()
	defer eal.Unlock()
	if !eal.initialized || eal.stopped {
		return nil
	}
	if eal.users != 0 {
		return fmt.Errorf("cannot clean up DPDK EAL while %d sources are running", eal.users)
	}
	if result := C.rte_eal_cleanup(); result < 0 {
		return fmt.Errorf("clean up DPDK EAL: %s", C.GoString(C.reflow_dpdk_error()))
	}
	for _, value := range eal.argumentStrings {
		C.free(value)
	}
	eal.argumentStrings = nil
	C.free(eal.argv)
	eal.argv = nil
	eal.stopped = true
	return nil
}
