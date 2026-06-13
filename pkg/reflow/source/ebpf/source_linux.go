//go:build linux && !reflow_noebpf

package ebpf

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

const (
	anyInterfaceName = "any"

	ethPAll = 0x0003

	packetHost      = 0
	packetBroadcast = 1
	packetMulticast = 2
	packetOtherHost = 3
	packetOutgoing  = 4
	packetLoopback  = 5
	packetUser      = 6
	packetKernel    = 7

	bpfProgLoad          = 5
	bpfMapCreate         = 0
	bpfMapUpdateElem     = 2
	bpfMapTypePerfEvent  = 4
	bpfProgTypeSocket    = 1
	bpfPseudoMapFD       = 1
	bpfLD                = 0x00
	bpfLDX               = 0x01
	bpfST                = 0x02
	bpfSTX               = 0x03
	bpfALU64             = 0x07
	bpfMov               = 0xb0
	bpfAdd               = 0x00
	bpfOr                = 0x40
	bpfLSh               = 0x60
	bpfK                 = 0x00
	bpfX                 = 0x08
	bpfDW                = 0x18
	bpfW                 = 0x00
	bpfMem               = 0x60
	bpfImm               = 0x00
	bpfExit              = 0x90
	bpfCall              = 0x80
	bpfJEQ               = 0x10
	bpfJLE               = 0xb0
	bpfJMP               = 0x05
	bpfReg0              = 0
	bpfReg1              = 1
	bpfReg2              = 2
	bpfReg3              = 3
	bpfReg4              = 4
	bpfReg5              = 5
	bpfReg6              = 6
	bpfReg7              = 7
	bpfReg10             = 10
	bpfFuncPerfOutput    = 25
	bpfLogBufferSize     = 16 * 1024
	defaultPollInterval  = 500 * time.Millisecond
	defaultPerfDataPages = 8

	perfRecordSample = 9

	skbLenOffset            = 0
	skbPacketTypeOffset     = 4
	skbMarkOffset           = 8
	skbQueueMappingOffset   = 12
	skbProtocolOffset       = 16
	skbPriorityOffset       = 32
	skbIngressIfindexOffset = 36
	skbIfindexOffset        = 40
	skbTCIndexOffset        = 44
	skbHashOffset           = 68
	skbTCClassIDOffset      = 72
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
	skb             skbMetadata
	hasSKBMetadata  bool
	conntrack       conntrackMetadata
	hasConntrack    bool
}

type skbMetadata struct {
	Len            uint32
	PacketType     uint32
	Mark           uint32
	QueueMapping   uint32
	Protocol       uint32
	Priority       uint32
	IngressIfindex uint32
	Ifindex        uint32
	TCIndex        uint32
	Hash           uint32
	TCClassID      uint32
}

type bpfMapCreateAttr struct {
	MapType    uint32
	KeySize    uint32
	ValueSize  uint32
	MaxEntries uint32
	MapFlags   uint32
	InnerMapFD uint32
	NumaNode   uint32
	MapName    [16]byte
	MapIfindex uint32
	BTFFD      uint32
	BTFKeyType uint32
	BTFValType uint32
	BTFVMLinux uint32
}

type bpfMapElemAttr struct {
	MapFD uint32
	Pad   uint32
	Key   uint64
	Value uint64
	Flags uint64
}

type socketFilterHandles struct {
	progFD int
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
	var interfaceFilter *regexp.Regexp
	if cfg.InterfaceFilter != "" {
		compiled, err := regexp.Compile(cfg.InterfaceFilter)
		if err != nil {
			return nil, fmt.Errorf("compile source.interface_filter: %w", err)
		}
		interfaceFilter = compiled
	}
	direction, err := config.NormalizeEBPFDirection(cfg.EBPF.Direction)
	if err != nil {
		return nil, err
	}
	cfg.EBPF.Direction = direction
	var conntrack *conntrackTracker
	if cfg.EBPF.ConntrackEnabled() {
		conntrack = newConntrackTracker(cfg.EBPF.ConntrackPath)
	}
	if cfg.Interface == anyInterfaceName {
		return &Source{
			cfg:                   cfg,
			agentIP:               firstSystemIP(),
			captureAny:            true,
			interfaceFilter:       interfaceFilter,
			interfaceNames:        make(map[uint32]string),
			initializedInterfaces: make(map[uint32]struct{}),
			fd:                    -1,
			progFD:                -1,
			eventMapFD:            -1,
			conntrack:             conntrack,
		}, nil
	}
	iface, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup capture interface %s: %w", cfg.Interface, err)
	}
	return &Source{
		cfg:                   cfg,
		agentIP:               firstInterfaceIP(iface),
		captureInterfaceIndex: iface.Index,
		interfaceFilter:       interfaceFilter,
		interfaceNames: map[uint32]string{
			uint32(iface.Index): iface.Name,
		},
		initializedInterfaces: make(map[uint32]struct{}),
		fd:                    -1,
		progFD:                -1,
		eventMapFD:            -1,
		conntrack:             conntrack,
	}, nil
}

// Start captures raw Ethernet frames from an AF_PACKET socket with a small eBPF
// socket filter attached. The filter emits one perf-buffer record containing
// both SKB metadata and packet bytes, so user space never has to pair a packet
// read with a separate metadata map lookup.
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

	eventMapFD, perfEvents, err := createPerfEventOutput(defaultPerfDataPages)
	if err != nil {
		return fmt.Errorf("create ebpf perf output: %w", err)
	}
	s.mu.Lock()
	s.eventMapFD = eventMapFD
	s.perfEvents = perfEvents
	s.mu.Unlock()

	filter, err := attachSocketFilter(fd, s.cfg.SnapLen, eventMapFD)
	if err != nil {
		return fmt.Errorf("attach packet socket filter: %w", err)
	}
	if filter.progFD >= 0 {
		s.mu.Lock()
		s.progFD = filter.progFD
		s.mu.Unlock()
	}

	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()

	return s.pollPerfEvents(ctx, perfEvents, emit)
}

func (s *Source) packetEvent(data []byte, meta packetMetadata) *event.Event {
	now := time.Now().UTC()
	samplePool := uint32(s.seenCount)
	payload := append([]byte(nil), data...)
	wireLength := len(payload)
	if meta.hasSKBMetadata && meta.skb.Len > uint32(wireLength) {
		wireLength = int(meta.skb.Len)
	}
	evt := &event.Event{
		ReceivedAt: now,
		Source: event.SourceMetadata{
			Network:               s.cfg.Network,
			Address:               s.cfg.Interface,
			Type:                  s.cfg.Type,
			CaptureInterface:      s.eventCaptureInterface(meta),
			CaptureInterfaceIndex: s.eventCaptureInterfaceIndex(meta),
			CaptureDirection:      meta.direction,
			CapturePacketType:     meta.packetType,
			AgentIP:               s.agentIP,
			SourceID:              s.eventSourceID(meta),
			SourceIDSet:           true,
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
			"capture_length": len(payload),
			"wire_length":    wireLength,
		},
	}
	applyPacketMetadataFields(evt.Fields, meta)
	if meta.hasConntrack {
		evt.Internal = make(map[string]any, 4)
		addTupleFields(evt.Internal, "conntrack_reply", meta.conntrack.reply)
	}
	return evt
}

func (s *Source) pollPerfEvents(ctx context.Context, readers []*perfEventReader, emit func(*event.Event) error) error {
	pollFDs := make([]unix.PollFd, 0, len(readers))
	for _, reader := range readers {
		pollFDs = append(pollFDs, unix.PollFd{Fd: int32(reader.fd), Events: unix.POLLIN})
	}
	timeout := int(defaultPollInterval / time.Millisecond)
	for {
		if ctx.Err() != nil {
			return nil
		}
		_, err := unix.Poll(pollFDs, timeout)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("poll ebpf perf events: %w", err)
		}
		for i := range readers {
			if pollFDs[i].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
				return fmt.Errorf("poll ebpf perf event cpu %d: revents=%#x", readers[i].cpu, pollFDs[i].Revents)
			}
			if err := readers[i].readSamples(func(sample []byte) error {
				return s.emitPerfSample(sample, emit)
			}); err != nil {
				return err
			}
		}
	}
}

func (s *Source) emitPerfSample(sample []byte, emit func(*event.Event) error) error {
	skb, packet, err := parsePerfPacketSample(sample)
	if err != nil {
		return err
	}
	meta := s.packetMetadataFromSKB(skb)
	if !allowDirection(s.cfg.EBPF.DirectionFilter(), meta.direction) {
		return nil
	}
	if !s.packetInterfaceAllowed(meta) {
		return nil
	}
	if s.captureAny {
		if err := s.emitFirstSeenInterfaceInits(meta, emit); err != nil {
			return err
		}
	}
	s.seenCount++
	if !s.shouldEmitCurrentPacket() {
		return nil
	}
	if s.conntrack != nil {
		if ct, ok := s.conntrack.Lookup(packet); ok {
			meta.conntrack = ct
			meta.hasConntrack = true
		}
	}
	return emit(s.packetEvent(packet, meta))
}

func (s *Source) emitFirstSeenInterfaceInits(meta packetMetadata, emit func(*event.Event) error) error {
	for _, item := range []struct {
		ifIndex uint32
		name    string
	}{
		{ifIndex: meta.inputIf, name: meta.inputInterface},
		{ifIndex: meta.outputIf, name: meta.outputInterface},
	} {
		if item.ifIndex == 0 || s.interfaceInitialized(item.ifIndex) {
			continue
		}
		name := item.name
		if name == "" {
			name = s.interfaceName(item.ifIndex)
		}
		if name == "" {
			name = fmt.Sprintf("ifindex-%d", item.ifIndex)
		}
		if !s.interfaceAllowed(name) {
			continue
		}
		s.markInterfaceInitialized(item.ifIndex, name)
		evt := s.sourceInitEvent(net.Interface{Index: int(item.ifIndex), Name: name}, s.agentIP, 0)
		if err := emit(evt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) packetInterfaceAllowed(meta packetMetadata) bool {
	if s.interfaceFilter == nil {
		return true
	}
	for _, item := range []struct {
		ifIndex uint32
		name    string
	}{
		{ifIndex: meta.inputIf, name: meta.inputInterface},
		{ifIndex: meta.outputIf, name: meta.outputInterface},
	} {
		name := item.name
		if name == "" && item.ifIndex != 0 {
			name = s.interfaceName(item.ifIndex)
		}
		if s.interfaceAllowed(name) {
			return true
		}
	}
	return false
}

func (s *Source) eventCaptureInterface(meta packetMetadata) string {
	switch meta.direction {
	case "out":
		if meta.outputInterface != "" {
			return meta.outputInterface
		}
		if meta.outputIf != 0 {
			if name := s.interfaceName(meta.outputIf); name != "" {
				return name
			}
		}
	default:
		if meta.inputInterface != "" {
			return meta.inputInterface
		}
		if meta.inputIf != 0 {
			if name := s.interfaceName(meta.inputIf); name != "" {
				return name
			}
		}
		if meta.outputInterface != "" {
			return meta.outputInterface
		}
		if meta.outputIf != 0 {
			if name := s.interfaceName(meta.outputIf); name != "" {
				return name
			}
		}
	}
	return s.cfg.Interface
}

func (s *Source) eventCaptureInterfaceIndex(meta packetMetadata) int {
	switch meta.direction {
	case "out":
		if meta.outputIf != 0 {
			return int(meta.outputIf)
		}
	default:
		if meta.inputIf != 0 {
			return int(meta.inputIf)
		}
		if meta.outputIf != 0 {
			return int(meta.outputIf)
		}
	}
	return s.captureInterfaceIndex
}

func (s *Source) eventSourceID(meta packetMetadata) uint32 {
	switch meta.direction {
	case "out":
		if meta.outputIf != 0 {
			return s.sourceIDForInterface(meta.outputIf)
		}
	default:
		if meta.inputIf != 0 {
			return s.sourceIDForInterface(meta.inputIf)
		}
		if meta.outputIf != 0 {
			return s.sourceIDForInterface(meta.outputIf)
		}
	}
	return s.sourceID()
}

func parsePerfPacketSample(sample []byte) (skbMetadata, []byte, error) {
	metadataSize := int(unsafe.Sizeof(skbMetadata{}))
	if len(sample) < metadataSize {
		return skbMetadata{}, nil, fmt.Errorf("short ebpf packet event: got %d bytes, need at least %d", len(sample), metadataSize)
	}
	meta := skbMetadata{
		Len:            binary.LittleEndian.Uint32(sample[0:4]),
		PacketType:     binary.LittleEndian.Uint32(sample[4:8]),
		Mark:           binary.LittleEndian.Uint32(sample[8:12]),
		QueueMapping:   binary.LittleEndian.Uint32(sample[12:16]),
		Protocol:       binary.LittleEndian.Uint32(sample[16:20]),
		Priority:       binary.LittleEndian.Uint32(sample[20:24]),
		IngressIfindex: binary.LittleEndian.Uint32(sample[24:28]),
		Ifindex:        binary.LittleEndian.Uint32(sample[28:32]),
		TCIndex:        binary.LittleEndian.Uint32(sample[32:36]),
		Hash:           binary.LittleEndian.Uint32(sample[36:40]),
		TCClassID:      binary.LittleEndian.Uint32(sample[40:44]),
	}
	packet := append([]byte(nil), sample[metadataSize:]...)
	return meta, packet, nil
}

func (s *Source) packetMetadataFromSKB(skb skbMetadata) packetMetadata {
	packetType := uint8(skb.PacketType)
	meta := packetMetadata{
		packetType: packetTypeName(packetType),
		direction:  "unknown",
	}
	switch packetType {
	case packetOutgoing:
		meta.direction = "out"
	case packetHost, packetBroadcast, packetMulticast, packetOtherHost:
		meta.direction = "in"
	case packetLoopback:
		meta.direction = "loopback"
	}
	if s.cfg.EBPF.SKBMetadataEnabled() {
		meta = s.mergeSKBMetadata(meta, skb)
	}
	switch meta.direction {
	case "in":
		if meta.inputIf == 0 {
			meta.inputIf = firstNonZero(skb.IngressIfindex, skb.Ifindex, uint32(s.captureInterfaceIndex))
			meta.inputInterface = s.interfaceName(meta.inputIf)
		}
	case "out":
		if meta.outputIf == 0 {
			meta.outputIf = firstNonZero(skb.Ifindex, uint32(s.captureInterfaceIndex))
			meta.outputInterface = s.interfaceName(meta.outputIf)
		}
	case "loopback":
		if meta.inputIf == 0 {
			meta.inputIf = firstNonZero(skb.IngressIfindex, skb.Ifindex, uint32(s.captureInterfaceIndex))
			meta.inputInterface = s.interfaceName(meta.inputIf)
		}
		if meta.outputIf == 0 {
			meta.outputIf = firstNonZero(skb.Ifindex, uint32(s.captureInterfaceIndex))
			meta.outputInterface = s.interfaceName(meta.outputIf)
		}
	}
	return meta
}

func firstNonZero(values ...uint32) uint32 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
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

func allowDirection(filter, direction string) bool {
	switch filter {
	case "", "both":
		return true
	case "ingress":
		return direction == "in"
	case "egress":
		return direction == "out"
	default:
		return true
	}
}

func (s *Source) mergeSKBMetadata(meta packetMetadata, skb skbMetadata) packetMetadata {
	meta.skb = skb
	meta.hasSKBMetadata = true
	if skb.PacketType != 0 {
		meta.packetType = packetTypeName(uint8(skb.PacketType))
	}
	if skb.IngressIfindex != 0 && meta.inputIf == 0 {
		meta.inputIf = skb.IngressIfindex
		meta.inputInterface = s.interfaceName(skb.IngressIfindex)
	}
	if skb.Ifindex != 0 {
		switch meta.direction {
		case "out":
			meta.outputIf = skb.Ifindex
			meta.outputInterface = s.interfaceName(skb.Ifindex)
		case "in", "loopback":
			if meta.inputIf == 0 {
				meta.inputIf = skb.Ifindex
				meta.inputInterface = s.interfaceName(skb.Ifindex)
			}
		default:
			meta.outputIf = skb.Ifindex
			meta.outputInterface = s.interfaceName(skb.Ifindex)
		}
	}
	if meta.inputInterface == "" && meta.inputIf != 0 {
		meta.inputInterface = s.interfaceName(meta.inputIf)
	}
	if meta.outputInterface == "" && meta.outputIf != 0 {
		meta.outputInterface = s.interfaceName(meta.outputIf)
	}
	return meta
}

func (s *Source) interfaceName(ifindex uint32) string {
	if ifindex == 0 {
		return ""
	}
	s.mu.Lock()
	if name := s.interfaceNames[ifindex]; name != "" {
		s.mu.Unlock()
		return name
	}
	s.mu.Unlock()
	iface, err := net.InterfaceByIndex(int(ifindex))
	if err != nil || iface == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.interfaceNames == nil {
		s.interfaceNames = make(map[uint32]string)
	}
	s.interfaceNames[ifindex] = iface.Name
	return iface.Name
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
	}
	if meta.outputInterface != "" {
		fields["output_interface"] = meta.outputInterface
	}
	if meta.hasSKBMetadata {
		fields["skb_len"] = meta.skb.Len
		fields["skb_packet_type"] = meta.skb.PacketType
		fields["skb_mark"] = meta.skb.Mark
		fields["skb_queue_mapping"] = meta.skb.QueueMapping
		fields["skb_protocol"] = meta.skb.Protocol
		fields["skb_priority"] = meta.skb.Priority
		fields["skb_ingress_ifindex"] = meta.skb.IngressIfindex
		fields["skb_ifindex"] = meta.skb.Ifindex
		fields["skb_tc_index"] = meta.skb.TCIndex
		fields["skb_hash"] = meta.skb.Hash
		fields["skb_tc_classid"] = meta.skb.TCClassID
	}
	if meta.hasConntrack {
		applyConntrackFields(fields, meta.conntrack)
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
	if s.eventMapFD >= 0 {
		if err := unix.Close(s.eventMapFD); err != nil && err != unix.EBADF && firstErr == nil {
			firstErr = err
		}
		s.eventMapFD = -1
	}
	for _, reader := range s.perfEvents {
		if err := reader.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	s.perfEvents = nil
	return firstErr
}

func loadSocketFilterProgram(snapLen, eventMapFD int, fullMetadata bool) (int, error) {
	if snapLen <= 0 {
		return -1, fmt.Errorf("snaplen must be > 0")
	}
	insns := socketFilterInstructions(snapLen, eventMapFD, fullMetadata)
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

func socketFilterInstructions(snapLen, eventMapFD int, fullMetadata bool) []bpfInsn {
	if eventMapFD < 0 {
		return []bpfInsn{
			mov64Imm(bpfReg0, int32(snapLen)),
			exitInsn(),
		}
	}
	metadataSize := int16(unsafe.Sizeof(skbMetadata{}))
	stackSize := alignInt16(metadataSize, 8)
	stackBase := -stackSize
	insns := []bpfInsn{
		mov64Reg(bpfReg6, bpfReg1),
	}
	fields := []struct {
		skbOffset   int16
		eventOffset int16
	}{
		{skbLenOffset, 0},
		{skbPacketTypeOffset, 4},
		{skbMarkOffset, 8},
		{skbQueueMappingOffset, 12},
		{skbProtocolOffset, 16},
		{skbPriorityOffset, 20},
		{skbIngressIfindexOffset, 24},
		{skbIfindexOffset, 28},
		{skbHashOffset, 36},
	}
	if fullMetadata {
		fields = append(fields,
			struct {
				skbOffset   int16
				eventOffset int16
			}{skbTCIndexOffset, 32},
			struct {
				skbOffset   int16
				eventOffset int16
			}{skbTCClassIDOffset, 40},
		)
	}
	for offset := int16(0); offset < stackSize; offset += 4 {
		insns = append(insns, storeImm(bpfReg10, stackBase+offset, 0))
	}
	for _, field := range fields {
		insns = append(insns,
			loadMem(bpfReg1, bpfReg6, field.skbOffset),
			storeReg(bpfReg10, bpfReg1, stackBase+field.eventOffset),
		)
	}
	insns = append(insns,
		loadMem(bpfReg3, bpfReg6, skbLenOffset),
		jumpImm(bpfJLE, bpfReg3, int32(snapLen), 1),
		mov64Imm(bpfReg3, int32(snapLen)),
		lsh64Imm(bpfReg3, 32),
	)
	insns = append(insns, loadImm64(bpfReg7, uint64(unix.BPF_F_CURRENT_CPU))...)
	insns = append(insns, or64Reg(bpfReg3, bpfReg7))
	insns = append(insns, loadMapFD(bpfReg2, eventMapFD)...)
	insns = append(insns,
		mov64Reg(bpfReg1, bpfReg6),
		mov64Reg(bpfReg4, bpfReg10),
		add64Imm(bpfReg4, int32(stackBase)),
		mov64Imm(bpfReg5, int32(metadataSize)),
		callInsn(bpfFuncPerfOutput),
		mov64Imm(bpfReg0, 0),
		exitInsn(),
	)
	return insns
}

func reg(dst, src uint8) uint8 {
	return dst | src<<4
}

func alignInt16(v, alignment int16) int16 {
	if alignment <= 0 || v%alignment == 0 {
		return v
	}
	return v + alignment - v%alignment
}

func mov64Imm(dst uint8, imm int32) bpfInsn {
	return bpfInsn{Code: bpfALU64 | bpfMov | bpfK, DstSrc: reg(dst, 0), Imm: imm}
}

func mov64Reg(dst, src uint8) bpfInsn {
	return bpfInsn{Code: bpfALU64 | bpfMov | bpfX, DstSrc: reg(dst, src)}
}

func add64Imm(dst uint8, imm int32) bpfInsn {
	return bpfInsn{Code: bpfALU64 | bpfAdd | bpfK, DstSrc: reg(dst, 0), Imm: imm}
}

func or64Reg(dst, src uint8) bpfInsn {
	return bpfInsn{Code: bpfALU64 | bpfOr | bpfX, DstSrc: reg(dst, src)}
}

func lsh64Imm(dst uint8, imm int32) bpfInsn {
	return bpfInsn{Code: bpfALU64 | bpfLSh | bpfK, DstSrc: reg(dst, 0), Imm: imm}
}

func loadMem(dst, src uint8, off int16) bpfInsn {
	return bpfInsn{Code: bpfLDX | bpfMem | bpfW, DstSrc: reg(dst, src), Off: off}
}

func storeImm(dst uint8, off int16, imm int32) bpfInsn {
	return bpfInsn{Code: bpfST | bpfMem | bpfW, DstSrc: reg(dst, 0), Off: off, Imm: imm}
}

func storeReg(dst, src uint8, off int16) bpfInsn {
	return bpfInsn{Code: bpfSTX | bpfMem | bpfW, DstSrc: reg(dst, src), Off: off}
}

func loadMapFD(dst uint8, mapFD int) []bpfInsn {
	return []bpfInsn{
		{Code: bpfLD | bpfDW | bpfImm, DstSrc: reg(dst, bpfPseudoMapFD), Imm: int32(mapFD)},
		{},
	}
}

func loadImm64(dst uint8, imm uint64) []bpfInsn {
	return []bpfInsn{
		{Code: bpfLD | bpfDW | bpfImm, DstSrc: reg(dst, 0), Imm: int32(imm)},
		{Imm: int32(imm >> 32)},
	}
}

func callInsn(helper int32) bpfInsn {
	return bpfInsn{Code: bpfJMP | bpfCall, Imm: helper}
}

func jumpImm(op uint8, dst uint8, imm int32, off int16) bpfInsn {
	return bpfInsn{Code: bpfJMP | op | bpfK, DstSrc: reg(dst, 0), Off: off, Imm: imm}
}

func exitInsn() bpfInsn {
	return bpfInsn{Code: bpfJMP | bpfExit}
}

func bpfProgLoadProgram(attr bpfProgLoadAttr) (int, unix.Errno) {
	fd, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(bpfProgLoad), uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	if errno != 0 {
		return -1, errno
	}
	return int(fd), 0
}

func attachSocketFilter(fd, snapLen, eventMapFD int) (socketFilterHandles, error) {
	progFD, err := loadSocketFilterProgram(snapLen, eventMapFD, true)
	if err != nil {
		progFD, err = loadSocketFilterProgram(snapLen, eventMapFD, false)
	}
	if err == nil {
		if attachErr := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, progFD); attachErr != nil {
			_ = unix.Close(progFD)
			return socketFilterHandles{}, fmt.Errorf("attach ebpf socket filter: %w", attachErr)
		}
		return socketFilterHandles{progFD: progFD}, nil
	}
	return socketFilterHandles{}, fmt.Errorf("load ebpf socket perf filter: %w", err)
}

func createPerfEventArrayMap(maxEntries int) (int, error) {
	attr := bpfMapCreateAttr{
		MapType:    bpfMapTypePerfEvent,
		KeySize:    4,
		ValueSize:  4,
		MaxEntries: uint32(maxEntries),
	}
	copy(attr.MapName[:], "reflow_events")
	fd, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(bpfMapCreate), uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}

func updatePerfEventArrayMap(mapFD int, cpu int, perfFD int) error {
	key := uint32(cpu)
	value := uint32(perfFD)
	attr := bpfMapElemAttr{
		MapFD: uint32(mapFD),
		Key:   uint64(uintptr(unsafe.Pointer(&key))),
		Value: uint64(uintptr(unsafe.Pointer(&value))),
		Flags: unix.BPF_ANY,
	}
	_, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(bpfMapUpdateElem), uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	if errno != 0 {
		return errno
	}
	return nil
}

type perfEventReader struct {
	cpu      int
	fd       int
	pageSize int
	data     []byte
}

func createPerfEventOutput(dataPages int) (int, []*perfEventReader, error) {
	cpus := onlineCPUs()
	if len(cpus) == 0 {
		return -1, nil, fmt.Errorf("no online CPUs found")
	}
	maxCPU := cpus[0]
	for _, cpu := range cpus[1:] {
		if cpu > maxCPU {
			maxCPU = cpu
		}
	}
	mapFD, err := createPerfEventArrayMap(maxCPU + 1)
	if err != nil {
		return -1, nil, err
	}
	readers := make([]*perfEventReader, 0, len(cpus))
	for _, cpu := range cpus {
		reader, err := openPerfEventReader(cpu, dataPages)
		if err != nil {
			closePerfEventReaders(readers)
			_ = unix.Close(mapFD)
			return -1, nil, fmt.Errorf("open perf event for cpu %d: %w", cpu, err)
		}
		if err := updatePerfEventArrayMap(mapFD, cpu, reader.fd); err != nil {
			_ = reader.Close()
			closePerfEventReaders(readers)
			_ = unix.Close(mapFD)
			return -1, nil, fmt.Errorf("populate perf event map for cpu %d: %w", cpu, err)
		}
		readers = append(readers, reader)
	}
	return mapFD, readers, nil
}

func openPerfEventReader(cpu, dataPages int) (*perfEventReader, error) {
	if dataPages <= 0 {
		dataPages = defaultPerfDataPages
	}
	attr := unix.PerfEventAttr{
		Type:        unix.PERF_TYPE_SOFTWARE,
		Size:        uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		Config:      unix.PERF_COUNT_SW_BPF_OUTPUT,
		Sample_type: unix.PERF_SAMPLE_RAW,
		Wakeup:      1,
	}
	fd, err := unix.PerfEventOpen(&attr, -1, cpu, -1, unix.PERF_FLAG_FD_CLOEXEC)
	if err != nil {
		return nil, err
	}
	pageSize := os.Getpagesize()
	data, err := unix.Mmap(fd, 0, pageSize*(dataPages+1), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	if err := unix.IoctlSetInt(fd, unix.PERF_EVENT_IOC_ENABLE, 0); err != nil {
		_ = unix.Munmap(data)
		_ = unix.Close(fd)
		return nil, err
	}
	return &perfEventReader{cpu: cpu, fd: fd, pageSize: pageSize, data: data}, nil
}

func closePerfEventReaders(readers []*perfEventReader) {
	for _, reader := range readers {
		_ = reader.Close()
	}
}

func (r *perfEventReader) Close() error {
	var firstErr error
	if r.fd >= 0 {
		if err := unix.IoctlSetInt(r.fd, unix.PERF_EVENT_IOC_DISABLE, 0); err != nil && !errors.Is(err, unix.EBADF) {
			firstErr = err
		}
	}
	if len(r.data) > 0 {
		if err := unix.Munmap(r.data); err != nil && firstErr == nil {
			firstErr = err
		}
		r.data = nil
	}
	if r.fd >= 0 {
		if err := unix.Close(r.fd); err != nil && err != unix.EBADF && firstErr == nil {
			firstErr = err
		}
		r.fd = -1
	}
	return firstErr
}

func (r *perfEventReader) readSamples(emit func([]byte) error) error {
	if len(r.data) < r.pageSize {
		return fmt.Errorf("perf event cpu %d is not mapped", r.cpu)
	}
	page := (*unix.PerfEventMmapPage)(unsafe.Pointer(&r.data[0]))
	head := atomic.LoadUint64(&page.Data_head)
	tail := atomic.LoadUint64(&page.Data_tail)
	dataOffset := int(page.Data_offset)
	dataSize := int(page.Data_size)
	if dataOffset == 0 {
		dataOffset = r.pageSize
	}
	if dataSize == 0 {
		dataSize = len(r.data) - dataOffset
	}
	if dataOffset < 0 || dataSize <= 0 || dataOffset+dataSize > len(r.data) {
		return fmt.Errorf("invalid perf ring layout for cpu %d", r.cpu)
	}
	ring := r.data[dataOffset : dataOffset+dataSize]
	for tail < head {
		record, size, err := readPerfRecord(ring, tail)
		if err != nil {
			return fmt.Errorf("read ebpf perf record cpu %d: %w", r.cpu, err)
		}
		tail += uint64(size)
		if len(record) < 12 || binary.LittleEndian.Uint32(record[0:4]) != perfRecordSample {
			continue
		}
		rawSize := int(binary.LittleEndian.Uint32(record[8:12]))
		if rawSize < 0 || 12+rawSize > len(record) {
			return fmt.Errorf("invalid ebpf perf sample size %d in record size %d", rawSize, len(record))
		}
		if err := emit(record[12 : 12+rawSize]); err != nil {
			return err
		}
	}
	atomic.StoreUint64(&page.Data_tail, tail)
	return nil
}

func readPerfRecord(ring []byte, tail uint64) ([]byte, uint16, error) {
	if len(ring) < 8 {
		return nil, 0, fmt.Errorf("perf ring is too small")
	}
	headerBytes := readPerfRingBytes(ring, tail, 8)
	size := binary.LittleEndian.Uint16(headerBytes[6:8])
	if size < 8 {
		return nil, 0, fmt.Errorf("invalid perf record size %d", size)
	}
	record := readPerfRingBytes(ring, tail, int(size))
	return record, size, nil
}

func readPerfRingBytes(ring []byte, offset uint64, size int) []byte {
	start := int(offset % uint64(len(ring)))
	if start+size <= len(ring) {
		return append([]byte(nil), ring[start:start+size]...)
	}
	out := make([]byte, size)
	n := copy(out, ring[start:])
	copy(out[n:], ring[:size-n])
	return out
}

func onlineCPUs() []int {
	data, err := os.ReadFile("/sys/devices/system/cpu/online")
	if err == nil {
		if cpus := parseCPUList(strings.TrimSpace(string(data))); len(cpus) > 0 {
			return cpus
		}
	}
	n := runtime.NumCPU()
	cpus := make([]int, 0, n)
	for i := 0; i < n; i++ {
		cpus = append(cpus, i)
	}
	return cpus
}

func parseCPUList(text string) []int {
	var cpus []int
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		startText, endText, ranged := strings.Cut(part, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < 0 {
			return nil
		}
		end := start
		if ranged {
			end, err = strconv.Atoi(endText)
			if err != nil || end < start {
				return nil
			}
		}
		for cpu := start; cpu <= end; cpu++ {
			cpus = append(cpus, cpu)
		}
	}
	return cpus
}

func htons(v uint16) uint16 {
	return (v << 8) | (v >> 8)
}
