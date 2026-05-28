//go:build linux && !reflow_noebpf

package ebpf

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
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
	bpfMapCreate        = 0
	bpfMapLookupElem    = 1
	bpfMapTypeArray     = 2
	bpfProgTypeSocket   = 1
	bpfPseudoMapFD      = 1
	bpfLD               = 0x00
	bpfLDX              = 0x01
	bpfST               = 0x02
	bpfSTX              = 0x03
	bpfALU64            = 0x07
	bpfMov              = 0xb0
	bpfAdd              = 0x00
	bpfK                = 0x00
	bpfX                = 0x08
	bpfDW               = 0x18
	bpfW                = 0x00
	bpfMem              = 0x60
	bpfImm              = 0x00
	bpfExit             = 0x90
	bpfCall             = 0x80
	bpfJEQ              = 0x10
	bpfJMP              = 0x05
	bpfReg0             = 0
	bpfReg1             = 1
	bpfReg2             = 2
	bpfReg6             = 6
	bpfReg10            = 10
	bpfFuncMapLookup    = 1
	bpfLogBufferSize    = 16 * 1024
	defaultPollInterval = 500 * time.Millisecond

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
}

type socketFilterHandles struct {
	progFD        int
	metadataMapFD int
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
	direction, err := config.NormalizeEBPFDirection(cfg.EBPF.Direction)
	if err != nil {
		return nil, err
	}
	cfg.EBPF.Direction = direction
	iface, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup capture interface %s: %w", cfg.Interface, err)
	}
	var conntrack *conntrackTracker
	if cfg.EBPF.ConntrackEnabled() {
		conntrack = newConntrackTracker(cfg.EBPF.ConntrackPath)
	}
	return &Source{
		cfg:                   cfg,
		agentIP:               firstInterfaceIP(iface),
		captureInterfaceIndex: iface.Index,
		interfaceNames: map[uint32]string{
			uint32(iface.Index): iface.Name,
		},
		fd:            -1,
		progFD:        -1,
		metadataMapFD: -1,
		conntrack:     conntrack,
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

	filter, err := attachSocketFilter(fd, s.cfg.SnapLen)
	if err != nil {
		return fmt.Errorf("attach packet socket filter: %w", err)
	}
	if filter.progFD >= 0 || filter.metadataMapFD >= 0 {
		s.mu.Lock()
		s.progFD = filter.progFD
		s.metadataMapFD = filter.metadataMapFD
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
		meta := s.packetMetadata(from)
		if !allowDirection(s.cfg.EBPF.DirectionFilter(), meta.direction) {
			continue
		}
		s.seenCount++
		if !s.shouldEmitCurrentPacket() {
			continue
		}
		if s.cfg.EBPF.SKBMetadataEnabled() {
			if skb, ok := s.readSKBMetadata(); ok {
				meta = s.mergeSKBMetadata(meta, skb)
			}
		}
		if s.conntrack != nil {
			if ct, ok := s.conntrack.Lookup(buf[:n]); ok {
				meta.conntrack = ct
				meta.hasConntrack = true
			}
		}
		if err := emit(s.packetEvent(buf[:n], meta)); err != nil {
			return err
		}
	}
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
			CaptureInterface:      s.cfg.Interface,
			CaptureInterfaceIndex: s.captureInterfaceIndex,
			CaptureDirection:      meta.direction,
			CapturePacketType:     meta.packetType,
			AgentIP:               s.agentIP,
			SourceID:              s.sourceID(),
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
	if name := s.interfaceNames[ifindex]; name != "" {
		return name
	}
	iface, err := net.InterfaceByIndex(int(ifindex))
	if err != nil || iface == nil {
		return ""
	}
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
	if s.metadataMapFD >= 0 {
		if err := unix.Close(s.metadataMapFD); err != nil && err != unix.EBADF && firstErr == nil {
			firstErr = err
		}
		s.metadataMapFD = -1
	}
	return firstErr
}

func (s *Source) readSKBMetadata() (skbMetadata, bool) {
	s.mu.Lock()
	mapFD := s.metadataMapFD
	s.mu.Unlock()
	if mapFD < 0 {
		return skbMetadata{}, false
	}
	meta, err := lookupSKBMetadata(mapFD)
	if err != nil {
		return skbMetadata{}, false
	}
	return meta, true
}

func loadSocketFilterProgram(snapLen, metadataMapFD int, fullMetadata bool) (int, error) {
	if snapLen <= 0 {
		return -1, fmt.Errorf("snaplen must be > 0")
	}
	insns := socketFilterInstructions(snapLen, metadataMapFD, fullMetadata)
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

func socketFilterInstructions(snapLen, metadataMapFD int, fullMetadata bool) []bpfInsn {
	if metadataMapFD < 0 {
		return []bpfInsn{
			mov64Imm(bpfReg0, int32(snapLen)),
			exitInsn(),
		}
	}
	insns := []bpfInsn{
		mov64Reg(bpfReg6, bpfReg1),
		storeImm(bpfReg10, -4, 0),
		mov64Reg(bpfReg2, bpfReg10),
		add64Imm(bpfReg2, -4),
	}
	fields := []struct {
		skbOffset int16
		mapOffset int16
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
				skbOffset int16
				mapOffset int16
			}{skbTCIndexOffset, 32},
			struct {
				skbOffset int16
				mapOffset int16
			}{skbTCClassIDOffset, 40},
		)
	}
	insns = append(insns, loadMapFD(bpfReg1, metadataMapFD)...)
	insns = append(insns,
		callInsn(bpfFuncMapLookup),
		jumpImm(bpfJEQ, bpfReg0, 0, int16(2*len(fields))),
	)
	for _, field := range fields {
		insns = append(insns,
			loadMem(bpfReg1, bpfReg6, field.skbOffset),
			storeReg(bpfReg0, bpfReg1, field.mapOffset),
		)
	}
	insns = append(insns, mov64Imm(bpfReg0, int32(snapLen)), exitInsn())
	return insns
}

func reg(dst, src uint8) uint8 {
	return dst | src<<4
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

func attachSocketFilter(fd, snapLen int) (socketFilterHandles, error) {
	metadataMapFD, mapErr := createSKBMetadataMap()
	if mapErr != nil {
		metadataMapFD = -1
	}
	progFD, err := loadSocketFilterProgram(snapLen, metadataMapFD, true)
	if err != nil && metadataMapFD >= 0 {
		progFD, err = loadSocketFilterProgram(snapLen, metadataMapFD, false)
	}
	if err == nil {
		if attachErr := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, progFD); attachErr != nil {
			_ = unix.Close(progFD)
			if metadataMapFD >= 0 {
				_ = unix.Close(metadataMapFD)
			}
			return socketFilterHandles{}, fmt.Errorf("attach ebpf socket filter: %w", attachErr)
		}
		return socketFilterHandles{progFD: progFD, metadataMapFD: metadataMapFD}, nil
	}
	metadataErr := err
	if metadataMapFD >= 0 {
		_ = unix.Close(metadataMapFD)
	}
	progFD, err = loadSocketFilterProgram(snapLen, -1, false)
	if err == nil {
		if attachErr := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, progFD); attachErr != nil {
			_ = unix.Close(progFD)
			return socketFilterHandles{}, fmt.Errorf("attach ebpf socket filter: %w", attachErr)
		}
		return socketFilterHandles{progFD: progFD, metadataMapFD: -1}, nil
	}
	if fallbackErr := attachClassicSocketFilter(fd, snapLen); fallbackErr != nil {
		return socketFilterHandles{}, fmt.Errorf("load ebpf socket filter: %w; load metadata filter: %v; attach classic socket filter fallback: %v", err, metadataErr, fallbackErr)
	}
	return socketFilterHandles{progFD: -1, metadataMapFD: -1}, nil
}

func createSKBMetadataMap() (int, error) {
	attr := bpfMapCreateAttr{
		MapType:    bpfMapTypeArray,
		KeySize:    4,
		ValueSize:  uint32(unsafe.Sizeof(skbMetadata{})),
		MaxEntries: 1,
	}
	copy(attr.MapName[:], "reflow_skb_meta")
	fd, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(bpfMapCreate), uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}

func lookupSKBMetadata(mapFD int) (skbMetadata, error) {
	key := uint32(0)
	var meta skbMetadata
	attr := bpfMapElemAttr{
		MapFD: uint32(mapFD),
		Key:   uint64(uintptr(unsafe.Pointer(&key))),
		Value: uint64(uintptr(unsafe.Pointer(&meta))),
	}
	_, _, errno := unix.Syscall(unix.SYS_BPF, uintptr(bpfMapLookupElem), uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr))
	if errno != 0 {
		return skbMetadata{}, errno
	}
	return meta, nil
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
