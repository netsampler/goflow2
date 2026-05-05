//go:build linux

package ebpf

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

const (
	ethPAll = 0x0003

	bpfProgLoad         = 5
	bpfProgTypeSocket   = 1
	bpfALU64            = 0x07
	bpfMov              = 0xb0
	bpfK                = 0x00
	bpfExit             = 0x90
	bpfJMP              = 0x05
	bpfReg0             = 0
	bpfLogBufferSize    = 16 * 1024
	defaultPollInterval = 500 * time.Millisecond
)

type bpfInsn struct {
	Code   uint8
	DstSrc uint8
	Off    int16
	Imm    int32
}

type bpfProgLoadAttr struct {
	ProgType           uint32
	InsnCnt            uint32
	Insns              uint64
	License            uint64
	LogLevel           uint32
	LogSize            uint32
	LogBuf             uint64
	KernVersion        uint32
	ProgFlags          uint32
	ProgName           [16]byte
	ProgIfindex        uint32
	ExpectedAttachType uint32
	ProgBTFD           uint32
	FuncInfoRecSize    uint32
	FuncInfo           uint64
	FuncInfoCnt        uint32
	LineInfoRecSize    uint32
	LineInfo           uint64
	LineInfoCnt        uint32
	AttachBTFID        uint32
	AttachProgFD       uint32
	FDArray            uint64
}

// New validates an eBPF live-capture source and precomputes interface metadata
// used in emitted events and source_init control records.
func New(cfg config.SourceConfig) (*Source, error) {
	if cfg.Network != "ebpf" {
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
	if cfg.Interface == "" {
		return nil, fmt.Errorf("source.interface is required when source.network=ebpf")
	}
	if cfg.Type == "" {
		cfg.Type = "bytes"
	}
	if cfg.Type != "bytes" {
		return nil, fmt.Errorf("source.type must be bytes when source.network=ebpf")
	}
	if cfg.SnapLen <= 0 {
		cfg.SnapLen = 65535
	}
	if cfg.SampleEvery <= 0 {
		cfg.SampleEvery = 1
	}
	iface, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup capture interface %s: %w", cfg.Interface, err)
	}
	return &Source{
		cfg:                   cfg,
		agentIP:               firstInterfaceIP(iface),
		captureInterfaceIndex: iface.Index,
		fd:                    -1,
		progFD:                -1,
	}, nil
}

// Start captures raw Ethernet frames from an AF_PACKET socket with a small eBPF
// socket filter attached. The filter returns the configured snaplen, so packet
// selection stays in-kernel while ReFlow keeps the same bytes-event path used by
// pcap_live.
func (s *Source) Start(ctx context.Context, emit func(*event.Event) error) error {
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC, int(htons(ethPAll)))
	if err != nil {
		return fmt.Errorf("open ebpf packet socket on %s: %w", s.cfg.Interface, err)
	}
	s.mu.Lock()
	s.fd = fd
	s.mu.Unlock()
	defer s.Close()

	if err := unix.Bind(fd, &unix.SockaddrLinklayer{
		Protocol: htons(ethPAll),
		Ifindex:  s.captureInterfaceIndex,
	}); err != nil {
		return fmt.Errorf("bind ebpf packet socket to %s: %w", s.cfg.Interface, err)
	}
	tv := unix.NsecToTimeval(defaultPollInterval.Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("set ebpf packet socket timeout: %w", err)
	}

	progFD, err := loadSocketFilterProgram(s.cfg.SnapLen)
	if err != nil {
		return fmt.Errorf("load ebpf socket filter: %w", err)
	}
	s.mu.Lock()
	s.progFD = progFD
	s.mu.Unlock()
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, progFD); err != nil {
		return fmt.Errorf("attach ebpf socket filter to %s: %w", s.cfg.Interface, err)
	}

	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	buf := make([]byte, s.cfg.SnapLen)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			if err == unix.EAGAIN || err == unix.EWOULDBLOCK || err == unix.EINTR {
				if ctx.Err() != nil {
					return nil
				}
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read ebpf packet: %w", err)
		}
		if n <= 0 {
			continue
		}
		s.seenCount++
		if !s.shouldEmitCurrentPacket() {
			continue
		}
		if err := emit(s.packetEvent(buf[:n])); err != nil {
			return err
		}
	}
}

func (s *Source) packetEvent(data []byte) *event.Event {
	now := time.Now().UTC()
	samplePool := uint32(s.seenCount)
	payload := append([]byte(nil), data...)
	return &event.Event{
		ReceivedAt: now,
		Source: event.SourceMetadata{
			Network:               s.cfg.Network,
			Address:               s.cfg.Interface,
			Type:                  s.cfg.Type,
			CaptureInterface:      s.cfg.Interface,
			CaptureInterfaceIndex: s.captureInterfaceIndex,
			JSON: event.JSONMetadata{
				Flavor: s.cfg.JSON.Flavor,
			},
		},
		SFlow: &event.SFlowMetadata{
			AgentIP:      s.agentIP,
			SamplingRate: uint32(s.cfg.SampleEvery),
			SamplePool:   samplePool,
			Drops:        0,
		},
		Payload: payload,
		Fields: map[string]any{
			"agent_ip":       s.agentIP,
			"sampling_rate":  uint32(s.cfg.SampleEvery),
			"sample_pool":    samplePool,
			"drops":          uint32(0),
			"capture_length": len(payload),
			"wire_length":    len(payload),
		},
	}
}

func (s *Source) Close() error {
	var firstErr error
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fd >= 0 {
		if err := unix.Close(s.fd); err != nil && err != unix.EBADF {
			firstErr = err
		}
		s.fd = -1
	}
	if s.progFD >= 0 {
		if err := unix.Close(s.progFD); err != nil && err != unix.EBADF && firstErr == nil {
			firstErr = err
		}
		s.progFD = -1
	}
	return firstErr
}

func loadSocketFilterProgram(snapLen int) (int, error) {
	if snapLen <= 0 {
		return -1, fmt.Errorf("snaplen must be > 0")
	}
	insns := []bpfInsn{
		{
			Code:   bpfALU64 | bpfMov | bpfK,
			DstSrc: bpfReg0,
			Imm:    int32(snapLen),
		},
		{
			Code: bpfJMP | bpfExit,
		},
	}
	license := []byte("GPL\x00")
	logBuf := make([]byte, bpfLogBufferSize)
	attr := bpfProgLoadAttr{
		ProgType: bpfProgTypeSocket,
		InsnCnt:  uint32(len(insns)),
		Insns:    uint64(uintptr(unsafe.Pointer(&insns[0]))),
		License:  uint64(uintptr(unsafe.Pointer(&license[0]))),
		LogLevel: 1,
		LogSize:  uint32(len(logBuf)),
		LogBuf:   uint64(uintptr(unsafe.Pointer(&logBuf[0]))),
	}
	copy(attr.ProgName[:], "reflow_capture")

	fd, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(bpfProgLoad), uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	if errno != 0 {
		msg := strings.TrimRight(string(logBuf), "\x00")
		if msg != "" {
			return -1, fmt.Errorf("%w: %s", errno, msg)
		}
		return -1, errno
	}
	return int(fd), nil
}

func htons(v uint16) uint16 {
	return (v << 8) | (v >> 8)
}
