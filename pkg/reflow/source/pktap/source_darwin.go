//go:build darwin && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package pktap

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"unsafe"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/bpf"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"golang.org/x/sys/unix"
)

type driver struct {
	cfg    config.SourceConfig
	mu     sync.Mutex
	closed bool
	bpf    *bpf.Driver
}

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	if cfg.Capture.Promiscuous != nil && *cfg.Capture.Promiscuous {
		return nil, fmt.Errorf("pktap does not support promiscuous mode")
	}
	return capture.New(cfg, &driver{cfg: cfg})
}

func ioctl(fd int, req uint, p unsafe.Pointer) error {
	// x/sys has no exported pointer ioctl for Darwin's private pktap requests.
	//lint:ignore SA1019 x/sys has no exported pointer ioctl for Darwin's private pktap requests.
	_, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(p)) //nolint:staticcheck
	if e != 0 {
		return e
	}
	return nil
}

func (d *driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.bpf != nil {
		return d.bpf.Close()
	}
	return nil
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	sock, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(sock) }()
	var ifreq [32]byte
	copy(ifreq[:16], "pktap")
	if err = ioctl(sock, unix.SIOCIFCREATE, unsafe.Pointer(&ifreq[0])); err != nil {
		return fmt.Errorf("create pktap interface: %w", err)
	}
	// This exact interface was created by us, so destruction cannot affect an
	// existing capture session owned by another process.
	defer func() { _ = ioctl(sock, unix.SIOCIFDESTROY, unsafe.Pointer(&ifreq[0])) }()
	name := cstring(ifreq[:16])
	interfaces := d.cfg.Capture.Pktap.Interfaces
	if len(interfaces) == 0 {
		interfaces = strings.Split(d.cfg.Capture.Interface, ",")
	}
	if len(interfaces) > 8 {
		return fmt.Errorf("pktap accepts at most 8 interface selectors")
	}
	type filter struct {
		Op, Param uint32
		Value     [24]byte
	}
	var filters [8]filter
	for i, target := range interfaces {
		filters[i].Op = 1
		if target == "all" || target == "any" {
			filters[i].Param = 1
		} else {
			if len(target) == 0 || len(target) >= 24 {
				return fmt.Errorf("invalid pktap interface selector %q", target)
			}
			filters[i].Param = 2
			copy(filters[i].Value[:], target)
		}
	}
	type ifdrv struct {
		Name     [16]byte
		Cmd, Len uintptr
		Data     unsafe.Pointer
	}
	req := ifdrv{Cmd: 3, Len: unsafe.Sizeof(filters), Data: unsafe.Pointer(&filters[0])}
	copy(req.Name[:], name)
	if err = ioctl(sock, unix.SIOCSDRVSPEC, unsafe.Pointer(&req)); err != nil {
		return fmt.Errorf("configure pktap selectors: %w", err)
	}
	cfg := d.cfg
	cfg.Capture.Interface = name
	// BPF's snaplen is applied before the pktap framing is stripped.
	cfg.Capture.SnapLen += 256
	reader := bpf.NewDriver(cfg, func(fd int) error {
		want := uint32(1)
		if e := ioctl(fd, 0xc004427f, unsafe.Pointer(&want)); e != nil {
			return fmt.Errorf("enable BPF pktap metadata: %w", e)
		}
		return nil
	})
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.bpf = reader
	d.mu.Unlock()
	indexes := make(map[string]int)
	return reader.Run(ctx, func(frame capture.Frame) error {
		frame = normalize(frame)
		if frame.Interface != "" {
			index, ok := indexes[frame.Interface]
			if !ok {
				if iface, err := net.InterfaceByName(frame.Interface); err == nil {
					index = iface.Index
					indexes[frame.Interface] = index
				}
			}
			frame.InterfaceIndex = index
		}
		if snap := d.cfg.Capture.SnapLen; snap > 0 && len(frame.Data) > snap {
			frame.Data = frame.Data[:snap]
			frame.CaptureLength = snap
		}
		return emit(frame)
	})
}

func cstring(data []byte) string {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	return string(data)
}
