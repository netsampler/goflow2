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

	packetHost      = 0
	packetBroadcast = 1
	packetMulticast = 2
	packetOtherHost = 3
	packetOutgoing  = 4
	packetLoopback  = 5
	packetUser      = 6
	packetKernel    = 7

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

type packetMetadata struct {
	packetType      string
	direction       string
	inputIf         uint32
	outputIf        uint32
	inputInterface  string
	outputInterface string
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

	progFD, err := attachSocketFilter(fd, s.cfg.SnapLen)
	if err != nil {
		return fmt.Errorf("attach packet socket filter: %w", err)
	}
	if progFD >= 0 {
		s.mu.Lock()
		s.progFD = progFD
		s.mu.Unlock()
	}

	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	buf := make([]byte, s.cfg.SnapLen)
	for {
		n, from, err := unix.Recvfrom(fd, buf, 0)
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
		if err := emit(s.packetEvent(buf[:n], s.packetMetadata(from))); err != nil {
			return err
		}
	}
}

func (s *Source) packetEvent(data []byte, meta packetMetadata) *event.Event {
	now := time.Now().UTC()
	samplePool := uint32(s.seenCount)
	payload := append([]byte(nil), data...)
	evt := &event.Event{
		ReceivedAt: now,
		Source: event.SourceMetadata{
			Network:               s.cfg.Network,
			Address:               s.cfg.Interface,
			Type:                  s.cfg.Type,
			CaptureInterface:      s.cfg.Interface,
			CaptureInterfaceIndex: s.captureInterfaceIndex,
			CaptureDirection:      meta.direction,
			CapturePacketType:     meta.packetType,
			AgentIP:               s.agentIP,
			SourceID:              uint32(s.captureInterfaceIndex),
			Sampling: &event.SamplingMetadata{
				Rate:       uint32(s.cfg.SampleEvery),
				SamplePool: samplePool,
				Drops:      0,
			},
			JSON: event.JSONMetadata{
				Flavor: s.cfg.JSON.Flavor,
			},
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
	applyPacketMetadataFields(evt.Fields, meta)
	return evt
}

func (s *Source) packetMetadata(from unix.Sockaddr) packetMetadata {
	meta := packetMetadata{
		packetType: "unknown",
		direction:  "unknown",
	}
	link, ok := from.(*unix.SockaddrLinklayer)
	if !ok || link == nil {
		return meta
	}

	ifIndex := uint32(s.captureInterfaceIndex)
	if link.Ifindex > 0 {
		ifIndex = uint32(link.Ifindex)
	}
	meta.packetType = packetTypeName(link.Pkttype)
	switch link.Pkttype {
	case packetOutgoing:
		meta.direction = "out"
		meta.outputIf = ifIndex
		meta.outputInterface = s.cfg.Interface
	case packetHost, packetBroadcast, packetMulticast, packetOtherHost:
		meta.direction = "in"
		meta.inputIf = ifIndex
		meta.inputInterface = s.cfg.Interface
	case packetLoopback:
		meta.direction = "loopback"
		meta.inputIf = ifIndex
		meta.outputIf = ifIndex
		meta.inputInterface = s.cfg.Interface
		meta.outputInterface = s.cfg.Interface
	}
	return meta
}

func packetTypeName(packetType uint8) string {
	switch packetType {
	case packetHost:
		return "host"
	case packetBroadcast:
		return "broadcast"
	case packetMulticast:
		return "multicast"
	case packetOtherHost:
		return "otherhost"
	case packetOutgoing:
		return "outgoing"
	case packetLoopback:
		return "loopback"
	case packetUser:
		return "user"
	case packetKernel:
		return "kernel"
	default:
		return "unknown"
	}
}

func applyPacketMetadataFields(fields map[string]any, meta packetMetadata) {
	if meta.packetType != "" {
		fields["capture_packet_type"] = meta.packetType
	}
	if meta.direction != "" {
		fields["capture_direction"] = meta.direction
	}
	if meta.inputIf != 0 {
		fields["input_if"] = meta.inputIf
	}
	if meta.outputIf != 0 {
		fields["output_if"] = meta.outputIf
	}
	if meta.inputInterface != "" {
		fields["input_interface"] = meta.inputInterface
		fields["src_interface"] = meta.inputInterface
	}
	if meta.outputInterface != "" {
		fields["output_interface"] = meta.outputInterface
		fields["dst_interface"] = meta.outputInterface
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
	attr := bpfProgLoadAttr{
		ProgType: bpfProgTypeSocket,
		InsnCnt:  uint32(len(insns)),
		Insns:    uint64(uintptr(unsafe.Pointer(&insns[0]))),
		License:  uint64(uintptr(unsafe.Pointer(&license[0]))),
	}
	copy(attr.ProgName[:], "reflow_capture")

	fd, errno := bpfProgLoadProgram(attr)
	if errno == 0 {
		return fd, nil
	}

	logBuf := make([]byte, bpfLogBufferSize)
	attr.LogLevel = 1
	attr.LogSize = uint32(len(logBuf))
	attr.LogBuf = uint64(uintptr(unsafe.Pointer(&logBuf[0])))
	fd, loggedErrno := bpfProgLoadProgram(attr)
	if loggedErrno == 0 {
		return fd, nil
	}
	if msg := strings.TrimRight(string(logBuf), "\x00"); msg != "" {
		return -1, fmt.Errorf("%w: %s", loggedErrno, msg)
	}
	return -1, errno
}

func bpfProgLoadProgram(attr bpfProgLoadAttr) (int, unix.Errno) {
	fd, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(bpfProgLoad), uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	if errno != 0 {
		return -1, errno
	}
	return int(fd), 0
}

func attachSocketFilter(fd, snapLen int) (int, error) {
	progFD, err := loadSocketFilterProgram(snapLen)
	if err == nil {
		if attachErr := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, progFD); attachErr != nil {
			_ = unix.Close(progFD)
			return -1, fmt.Errorf("attach ebpf socket filter: %w", attachErr)
		}
		return progFD, nil
	}
	if fallbackErr := attachClassicSocketFilter(fd, snapLen); fallbackErr != nil {
		return -1, fmt.Errorf("load ebpf socket filter: %w; attach classic socket filter fallback: %v", err, fallbackErr)
	}
	return -1, nil
}

func attachClassicSocketFilter(fd, snapLen int) error {
	if snapLen <= 0 {
		return fmt.Errorf("snaplen must be > 0")
	}
	filter := []unix.SockFilter{
		{
			Code: uint16(unix.BPF_RET | unix.BPF_K),
			K:    uint32(snapLen),
		},
	}
	return unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &unix.SockFprog{
		Len:    uint16(len(filter)),
		Filter: &filter[0],
	})
}

func htons(v uint16) uint16 {
	return (v << 8) | (v >> 8)
}
