//go:build reflow_pcap && cgo && !reflow_dpdk && !reflow_pfring

package pcap

/*
#cgo !windows LDFLAGS: -lpcap
#cgo windows LDFLAGS: -lwpcap
#include <stdlib.h>
#include <stdint.h>
#include <pcap.h>

// Access pcap's native structures in C so timestamps and pointer layout follow
// the target SDK ABI, including Windows ARM64.
typedef struct {
    const unsigned char *data;
    uint32_t captured;
    uint32_t length;
    int64_t seconds;
    int64_t microseconds;
} reflow_pcap_packet;

static int reflow_pcap_read(pcap_t *handle, reflow_pcap_packet *packet) {
    struct pcap_pkthdr *header;
    const unsigned char *data;
    int result = pcap_next_ex(handle, &header, &data);
    if (result == 1) {
        packet->data = data;
        packet->captured = header->caplen;
        packet->length = header->len;
        packet->seconds = header->ts.tv_sec;
        packet->microseconds = header->ts.tv_usec;
    }
    return result;
}
*/
import "C"

import (
	"context"
	"fmt"
	"io"
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
	handle *C.pcap_t
	closed bool
}

func SDKVersion() string { return C.GoString(C.pcap_lib_version()) }

func (d *driver) open() (uint32, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, io.EOF
	}
	name := C.CString(d.cfg.Capture.Interface)
	defer C.free(unsafe.Pointer(name))
	var errbuf [C.PCAP_ERRBUF_SIZE]C.char
	handle := C.pcap_create(name, &errbuf[0])
	if handle == nil {
		return 0, fmt.Errorf("create libpcap capture: %s", C.GoString(&errbuf[0]))
	}
	fail := func(op string, result C.int) (uint32, error) {
		message := C.GoString(C.pcap_geterr(handle))
		C.pcap_close(handle)
		return 0, fmt.Errorf("%s: %s (status %d)", op, message, int(result))
	}
	if result := C.pcap_set_snaplen(handle, C.int(d.cfg.Capture.SnapLen)); result != 0 {
		return fail("set libpcap snaplen", result)
	}
	promisc := C.int(1)
	if d.cfg.Capture.Promiscuous != nil && !*d.cfg.Capture.Promiscuous {
		promisc = 0
	}
	if result := C.pcap_set_promisc(handle, promisc); result != 0 {
		return fail("set libpcap promiscuous mode", result)
	}
	if result := C.pcap_set_timeout(handle, 100); result != 0 {
		return fail("set libpcap timeout", result)
	}
	if result := C.pcap_activate(handle); result < 0 {
		return fail("activate libpcap capture", result)
	}
	direction := C.pcap_direction_t(C.PCAP_D_INOUT)
	switch d.cfg.Capture.Direction {
	case "ingress":
		direction = C.PCAP_D_IN
	case "egress":
		direction = C.PCAP_D_OUT
	}
	if d.cfg.Capture.Direction != "" && d.cfg.Capture.Direction != "both" {
		if result := C.pcap_setdirection(handle, direction); result < 0 {
			return fail("set libpcap direction", result)
		}
	}
	if result := C.pcap_setnonblock(handle, 1, &errbuf[0]); result < 0 {
		return fail("set libpcap nonblocking mode", result)
	}
	link := uint32(C.pcap_datalink(handle))
	d.handle = handle
	return link, nil
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	link, err := d.open()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	ifIndex := 0
	if iface, err := net.InterfaceByName(d.cfg.Capture.Interface); err == nil {
		ifIndex = iface.Index
	}
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		frame, result, err := d.read(link, ifIndex)
		if err != nil {
			return err
		}
		switch result {
		case 1:
			if err := emit(frame); err != nil {
				return err
			}
		case 0:
			select {
			case <-ctx.Done():
				return nil
			case <-timer.C:
			}
		default:
			return nil
		}
	}
}

func (d *driver) read(link uint32, ifIndex int) (capture.Frame, int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.handle == nil {
		return capture.Frame{}, -2, nil
	}
	var packet C.reflow_pcap_packet
	result := C.reflow_pcap_read(d.handle, &packet)
	if result == -1 {
		return capture.Frame{}, int(result), fmt.Errorf("read libpcap packet: %s", C.GoString(C.pcap_geterr(d.handle)))
	}
	if result != 1 {
		return capture.Frame{}, int(result), nil
	}
	var stats C.struct_pcap_stat
	var drops uint64
	if C.pcap_stats(d.handle, &stats) == 0 {
		drops = uint64(stats.ps_drop) + uint64(stats.ps_ifdrop)
	}
	frame := capture.Frame{
		Data:           C.GoBytes(unsafe.Pointer(packet.data), C.int(packet.captured)),
		Timestamp:      time.Unix(int64(packet.seconds), int64(packet.microseconds)*1000).UTC(),
		LinkType:       link,
		Interface:      d.cfg.Capture.Interface,
		InterfaceIndex: ifIndex,
		CaptureLength:  int(packet.captured),
		WireLength:     int(packet.length),
		Drops:          drops,
	}
	if d.cfg.Capture.Interface == "any" {
		frame.Interface = ""
	}
	if d.cfg.Capture.Direction != "both" {
		frame.Direction = d.cfg.Capture.Direction
	}
	return frame, 1, nil
}

func (d *driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.handle != nil {
		C.pcap_close(d.handle)
		d.handle = nil
	}
	return nil
}
