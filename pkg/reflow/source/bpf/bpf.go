//go:build (darwin || freebsd || openbsd || netbsd) && !reflow_pcap && !reflow_dpdk

// Package bpf captures packets using the operating system's BPF device.
package bpf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

type Driver struct {
	cfg       config.SourceConfig
	configure func(int) error
	done      chan struct{}
	once      sync.Once
}

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	return capture.New(cfg, NewDriver(cfg, nil))
}

// NewDriver also serves pktap, which configures its BPF descriptor before bind.
func NewDriver(cfg config.SourceConfig, configure func(int) error) *Driver {
	return &Driver{cfg: cfg, configure: configure, done: make(chan struct{})}
}

func (d *Driver) Close() error { d.once.Do(func() { close(d.done) }); return nil }

func (d *Driver) stopped(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-d.done:
		return true
	default:
		return false
	}
}

func ioctl(fd int, req uint, data unsafe.Pointer) error {
	// x/sys has no exported pointer ioctl for BPF's OS-specific request structures.
	//lint:ignore SA1019 x/sys has no exported pointer ioctl for the BPF request structures.
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(data)) //nolint:staticcheck
	if errno != 0 {
		return errno
	}
	return nil
}

func openDevice() (int, error) {
	fd, err := unix.Open("/dev/bpf", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err == nil {
		return fd, nil
	}
	if !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.EBUSY) {
		return -1, err
	}
	for i := 0; i < 256; i++ {
		fd, err = unix.Open(fmt.Sprintf("/dev/bpf%d", i), unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		if !errors.Is(err, unix.EBUSY) {
			return -1, err
		}
	}
	return -1, fmt.Errorf("all BPF descriptors are busy")
}

func (d *Driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	if d.stopped(ctx) {
		return nil
	}
	iface, err := net.InterfaceByName(d.cfg.Capture.Interface)
	if err != nil {
		return fmt.Errorf("lookup BPF interface: %w", err)
	}
	fd, err := openDevice()
	if err != nil {
		return fmt.Errorf("open BPF device: %w", err)
	}
	// The reader owns the descriptor until it exits. Close wakes the reader by
	// signalling done, avoiding a concurrent close/read descriptor-reuse race.
	defer func() { _ = unix.Close(fd) }()
	if d.configure != nil {
		if err = d.configure(fd); err != nil {
			return err
		}
	}
	var ifreq [256]byte
	copy(ifreq[:unix.IFNAMSIZ-1], iface.Name)
	if err = ioctl(fd, unix.BIOCSETIF, unsafe.Pointer(&ifreq[0])); err != nil {
		return fmt.Errorf("bind BPF: %w", err)
	}
	one := uint32(1)
	if err = ioctl(fd, unix.BIOCIMMEDIATE, unsafe.Pointer(&one)); err != nil {
		return fmt.Errorf("BPF immediate mode: %w", err)
	}
	if d.cfg.Capture.Promiscuous != nil && *d.cfg.Capture.Promiscuous {
		if err = ioctl(fd, unix.BIOCPROMISC, nil); err != nil {
			return fmt.Errorf("BPF promiscuous mode: %w", err)
		}
	}
	if err = setDirection(fd, d.cfg.Capture.Direction); err != nil {
		return err
	}
	var version [2]uint16
	if err = ioctl(fd, unix.BIOCVERSION, unsafe.Pointer(&version[0])); err != nil {
		return fmt.Errorf("BPF version: %w", err)
	}
	if version[0] != 1 || version[1] < 1 {
		return fmt.Errorf("unsupported BPF version %d.%d", version[0], version[1])
	}
	if snap := d.cfg.Capture.SnapLen; snap > 0 {
		insn := unix.BpfInsn{Code: 6, K: uint32(snap)} // BPF_RET | BPF_K
		program := unix.BpfProgram{Len: 1, Insns: &insn}
		if err = ioctl(fd, unix.BIOCSETF, unsafe.Pointer(&program)); err != nil {
			return fmt.Errorf("BPF snapshot filter: %w", err)
		}
	}
	var link, buflen uint32
	if err = ioctl(fd, unix.BIOCGDLT, unsafe.Pointer(&link)); err != nil {
		return fmt.Errorf("BPF link type: %w", err)
	}
	if err = ioctl(fd, unix.BIOCGBLEN, unsafe.Pointer(&buflen)); err != nil {
		return fmt.Errorf("BPF buffer length: %w", err)
	}
	if buflen == 0 || buflen > 64*1024*1024 {
		return fmt.Errorf("unsupported BPF buffer length %d", buflen)
	}
	buf := make([]byte, int(buflen))
	poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	var stats [2]uint32
	for !d.stopped(ctx) {
		_, err = unix.Poll(poll, 100)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return fmt.Errorf("poll BPF: %w", err)
		}
		if poll[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, e := unix.Read(fd, buf)
		if errors.Is(e, unix.EAGAIN) || errors.Is(e, unix.EINTR) {
			continue
		}
		if e != nil {
			return fmt.Errorf("read BPF: %w", e)
		}
		if e = ioctl(fd, unix.BIOCGSTATS, unsafe.Pointer(&stats[0])); e != nil {
			return fmt.Errorf("BPF statistics: %w", e)
		}
		err = readRecords(buf[:n], timestampSize(), unix.BPF_ALIGNMENT, func(frame capture.Frame) error {
			frame.LinkType = link
			// BSD's native DLT_RAW is 12, while portable LINKTYPE_RAW is 101.
			if link == 12 {
				frame.LinkType = 101
			}
			frame.Interface = iface.Name
			frame.InterfaceIndex = iface.Index
			frame.Drops = uint64(stats[1])
			if direction := d.cfg.Capture.Direction; direction == "ingress" || direction == "egress" {
				// The OS filter selected this direction. Ordinary BPF record
				// headers do not otherwise provide per-packet direction.
				frame.Direction = direction
			}
			if snap := d.cfg.Capture.SnapLen; snap > 0 && len(frame.Data) > snap {
				frame.Data = frame.Data[:snap]
				frame.CaptureLength = snap
			}
			return emit(frame)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
