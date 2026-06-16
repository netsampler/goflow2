//go:build !reflow_nopcap && cgo

package pcap

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

const (
	anyInterfaceName           = "any"
	linkTypeLinuxSLL2          = 276
	linkTypeLinuxSLL2Truncated = 20
)

type Source struct {
	cfg                   config.SourceConfig
	agentIP               string
	handle                *pcap.Handle
	captureInterfaceIndex int
	captureAny            bool
	linkType              uint32
	interfaceFilter       *regexp.Regexp
	interfaceMu           sync.Mutex
	interfaceNames        map[uint32]string
	initializedInterfaces map[uint32]struct{}
	wg                    sync.WaitGroup
	seenCount             uint64
}

type cookedMetadata struct {
	linkType     uint32
	linkTypeName string
	headerLength int
	packetType   string
	protocol     uint16
	ifIndex      uint32
	ifName       string
	inputIf      uint32
	outputIf     uint32
	inputName    string
	outputName   string
}

// New validates a live-capture source and precomputes interface metadata used
// in emitted events and source_init control records.
func New(cfg config.SourceConfig) (*Source, error) {
	if cfg.Network != "pcap_live" {
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
	if cfg.Interface == "" {
		return nil, fmt.Errorf("source.interface is required when source.network=pcap_live")
	}
	if cfg.Type == "" {
		cfg.Type = "bytes"
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
	if cfg.Interface == anyInterfaceName {
		agentIP := firstSystemIP()
		return &Source{
			cfg:                   cfg,
			agentIP:               agentIP,
			captureAny:            true,
			interfaceFilter:       interfaceFilter,
			interfaceNames:        make(map[uint32]string),
			initializedInterfaces: make(map[uint32]struct{}),
		}, nil
	}
	iface, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup capture interface %s: %w", cfg.Interface, err)
	}
	agentIP := firstInterfaceIP(iface)
	return &Source{
		cfg:                   cfg,
		agentIP:               agentIP,
		captureInterfaceIndex: iface.Index,
		interfaceFilter:       interfaceFilter,
		interfaceNames: map[uint32]string{
			uint32(iface.Index): iface.Name,
		},
		initializedInterfaces: make(map[uint32]struct{}),
	}, nil
}

// InitEvents emits a source_init control event so template-based encoders can
// learn source-scoped metadata before the first captured packet arrives.
func (s *Source) InitEvents() ([]*event.Event, error) {
	if !s.captureAny {
		iface := net.Interface{
			Index: s.captureInterfaceIndex,
			Name:  s.cfg.Interface,
		}
		s.markInterfaceInitialized(uint32(iface.Index), iface.Name)
		return []*event.Event{s.sourceInitEvent(iface, s.agentIP, 0)}, nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list capture interfaces: %w", err)
	}
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].Index < ifaces[j].Index
	})
	events := make([]*event.Event, 0, len(ifaces))
	for _, iface := range ifaces {
		if !s.interfaceAllowed(iface.Name) {
			continue
		}
		agentIP := firstInterfaceIP(&iface)
		s.markInterfaceInitialized(uint32(iface.Index), iface.Name)
		events = append(events, s.sourceInitEvent(iface, agentIP, 0))
	}
	return events, nil
}

// Start reads packets from libpcap, applies packet-level sampling, and emits
// one raw bytes event per accepted capture.
func (s *Source) Start(ctx context.Context, emit func(*event.Event) error) error {
	handle, err := pcap.OpenLive(s.cfg.Interface, int32(s.cfg.SnapLen), true, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("open pcap device %s: %w", s.cfg.Interface, err)
	}
	s.handle = handle
	s.linkType = s.normalizeHandleLinkType(handle.LinkType())

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		if s.handle != nil {
			s.handle.Close()
		}
	}()

	for {
		data, ci, err := s.handle.ReadPacketData()
		if err != nil {
			switch err {
			case pcap.NextErrorTimeoutExpired:
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			case pcap.NextErrorNotActivated:
				return fmt.Errorf("read pcap packet: %w", err)
			default:
				select {
				case <-ctx.Done():
					return nil
				default:
					return fmt.Errorf("read pcap packet: %w", err)
				}
			}
		}
		payload, cooked := s.translatePacket(data)
		if !s.cookedInterfaceAllowed(cooked) {
			continue
		}
		// Drop counters come from the capture engine, not from packet contents.
		drops := s.currentDropCount()
		if s.captureAny && cooked.ifIndex != 0 && !s.interfaceInitialized(cooked.ifIndex) {
			if evt := s.dynamicSourceInitEvent(cooked, drops); evt != nil {
				if err := emit(evt); err != nil {
					return err
				}
			}
		}
		s.seenCount++
		if !s.shouldEmitCurrentPacket() {
			continue
		}
		sourceID := s.sourceIDForInterface(cooked.ifIndex)

		evt := &event.Event{
			ReceivedAt: time.Now().UTC(),
			Source: event.SourceMetadata{
				Network:               s.cfg.Network,
				Address:               s.cfg.Interface,
				Type:                  s.cfg.Type,
				CaptureInterface:      s.eventCaptureInterface(cooked),
				CaptureInterfaceIndex: s.eventCaptureInterfaceIndex(cooked),
				CapturePacketType:     cooked.packetType,
				AgentIP:               s.agentIP,
				SourceID:              sourceID,
				SourceIDSet:           true,
				Sampling: &event.SamplingMetadata{
					Rate:       uint32(s.cfg.SampleEvery),
					SamplePool: uint32(s.seenCount),
					Drops:      drops,
				},
				JSON: event.JSONMetadata{
					Flavor: s.cfg.JSON.Flavor,
				},
			},
			Payload: payload,
			Fields: map[string]any{
				"capture_length": ci.CaptureLength,
				"wire_length":    ci.Length,
			},
		}
		s.applyPcapMetadata(evt.Fields, cooked)

		if err := emit(evt); err != nil {
			return err
		}
	}
}

// currentDropCount asks libpcap for the current cumulative drop counters and
// folds both capture and interface drops into one exported value.
func (s *Source) currentDropCount() uint32 {
	if s.handle == nil {
		return 0
	}
	stats, err := s.handle.Stats()
	if err != nil || stats == nil {
		return 0
	}
	return uint32(stats.PacketsDropped + stats.PacketsIfDropped)
}

// shouldEmitCurrentPacket applies deterministic packet sampling based on
// sample_every/sample_offset so replay and testing stay reproducible.
func (s *Source) shouldEmitCurrentPacket() bool {
	if s.cfg.SampleEvery <= 1 {
		return true
	}
	index := int((s.seenCount - 1) % uint64(s.cfg.SampleEvery))
	return index == s.cfg.SampleOffset
}

// Close terminates the capture handle and waits for the shutdown goroutine.
func (s *Source) Close() error {
	if s.handle != nil {
		s.handle.Close()
	}
	s.wg.Wait()
	return nil
}

func (s *Source) sourceID() uint32 {
	return s.sourceIDForInterface(0)
}

func (s *Source) sourceIDForInterface(ifIndex uint32) uint32 {
	if s.cfg.SourceID != nil {
		return *s.cfg.SourceID
	}
	if s.captureAny && ifIndex != 0 {
		return ifIndex
	}
	return uint32(s.captureInterfaceIndex)
}

func (s *Source) sourceInitEvent(iface net.Interface, agentIP string, drops uint32) *event.Event {
	ifIndex := uint32(iface.Index)
	sourceID := s.sourceIDForInterface(ifIndex)
	return &event.Event{
		ReceivedAt: time.Now().UTC(),
		Kind:       "control",
		Source: event.SourceMetadata{
			Network:               s.cfg.Network,
			Address:               s.cfg.Interface,
			Type:                  s.cfg.Type,
			CaptureInterface:      iface.Name,
			CaptureInterfaceIndex: iface.Index,
			AgentIP:               agentIP,
			SourceID:              sourceID,
			SourceIDSet:           true,
			Sampling: &event.SamplingMetadata{
				Rate:       uint32(s.cfg.SampleEvery),
				SamplePool: 0,
				Drops:      drops,
			},
		},
		Control: &event.ControlMetadata{
			Type:   "source_init",
			Stream: "options_data",
		},
		Fields: map[string]any{
			"input_if":  ifIndex,
			"output_if": ifIndex,
		},
		Payload: event.SourceInit{
			Stream:       "options_data",
			AgentIP:      agentIP,
			SourceID:     sourceID,
			SamplingRate: uint32(s.cfg.SampleEvery),
			SamplePool:   0,
			Drops:        drops,
			InputIf:      ifIndex,
			OutputIf:     ifIndex,
		},
	}
}

func (s *Source) dynamicSourceInitEvent(cooked cookedMetadata, drops uint32) *event.Event {
	name := cooked.ifName
	if name == "" {
		name = s.interfaceName(cooked.ifIndex)
	}
	if name == "" {
		name = fmt.Sprintf("ifindex-%d", cooked.ifIndex)
	}
	if !s.interfaceAllowed(name) {
		return nil
	}
	s.markInterfaceInitialized(cooked.ifIndex, name)
	return s.sourceInitEvent(net.Interface{Index: int(cooked.ifIndex), Name: name}, s.agentIP, drops)
}

func (s *Source) markInterfaceInitialized(ifIndex uint32, name string) {
	if ifIndex == 0 {
		return
	}
	s.interfaceMu.Lock()
	defer s.interfaceMu.Unlock()
	if s.interfaceNames == nil {
		s.interfaceNames = make(map[uint32]string)
	}
	if name != "" {
		s.interfaceNames[ifIndex] = name
	}
	if s.initializedInterfaces == nil {
		s.initializedInterfaces = make(map[uint32]struct{})
	}
	s.initializedInterfaces[ifIndex] = struct{}{}
}

func (s *Source) interfaceInitialized(ifIndex uint32) bool {
	if ifIndex == 0 {
		return true
	}
	s.interfaceMu.Lock()
	defer s.interfaceMu.Unlock()
	_, ok := s.initializedInterfaces[ifIndex]
	return ok
}

func (s *Source) interfaceName(ifIndex uint32) string {
	if ifIndex == 0 {
		return ""
	}
	s.interfaceMu.Lock()
	defer s.interfaceMu.Unlock()
	if name := s.interfaceNames[ifIndex]; name != "" {
		return name
	}
	iface, err := net.InterfaceByIndex(int(ifIndex))
	if err != nil || iface == nil {
		return ""
	}
	if s.interfaceNames == nil {
		s.interfaceNames = make(map[uint32]string)
	}
	s.interfaceNames[ifIndex] = iface.Name
	return iface.Name
}

func (s *Source) interfaceAllowed(name string) bool {
	if s.interfaceFilter == nil {
		return true
	}
	return name != "" && s.interfaceFilter.MatchString(name)
}

func (s *Source) cookedInterfaceAllowed(cooked cookedMetadata) bool {
	if s.interfaceFilter == nil || cooked.ifIndex == 0 {
		return true
	}
	name := cooked.ifName
	if name == "" {
		name = s.interfaceName(cooked.ifIndex)
	}
	return s.interfaceAllowed(name)
}

func (s *Source) eventCaptureInterface(cooked cookedMetadata) string {
	if cooked.ifName != "" {
		return cooked.ifName
	}
	if cooked.ifIndex != 0 {
		if name := s.interfaceName(cooked.ifIndex); name != "" {
			return name
		}
	}
	return s.cfg.Interface
}

func (s *Source) eventCaptureInterfaceIndex(cooked cookedMetadata) int {
	if cooked.ifIndex != 0 {
		return int(cooked.ifIndex)
	}
	return s.captureInterfaceIndex
}

func (s *Source) normalizeHandleLinkType(linkType layers.LinkType) uint32 {
	value := uint32(linkType)
	if s.captureAny && value == linkTypeLinuxSLL2Truncated && pcap.DatalinkNameToVal("LINUX_SLL2") == linkTypeLinuxSLL2 {
		return linkTypeLinuxSLL2
	}
	return value
}

func (s *Source) translatePacket(data []byte) ([]byte, cookedMetadata) {
	cooked := cookedMetadata{
		linkType:     s.linkType,
		linkTypeName: pcapLinkTypeName(s.linkType),
	}
	switch s.linkType {
	case uint32(layers.LinkTypeLinuxSLL):
		return translateLinuxSLL(data, cooked)
	case linkTypeLinuxSLL2:
		return s.translateLinuxSLL2(data, cooked)
	default:
		return append([]byte(nil), data...), cooked
	}
}

func translateLinuxSLL(data []byte, cooked cookedMetadata) ([]byte, cookedMetadata) {
	if len(data) < 16 {
		return append([]byte(nil), data...), cooked
	}
	cooked.headerLength = 16
	cooked.packetType = linuxSLLPacketTypeName(uint8(binary.BigEndian.Uint16(data[0:2])))
	cooked.protocol = binary.BigEndian.Uint16(data[14:16])
	return synthesizeEthernetFromCooked(data[16:], cooked.protocol, data[6:14]), cooked
}

func (s *Source) translateLinuxSLL2(data []byte, cooked cookedMetadata) ([]byte, cookedMetadata) {
	if len(data) < 20 {
		return append([]byte(nil), data...), cooked
	}
	cooked.headerLength = 20
	cooked.protocol = binary.BigEndian.Uint16(data[0:2])
	cooked.ifIndex = binary.BigEndian.Uint32(data[4:8])
	cooked.packetType = linuxSLLPacketTypeName(data[10])
	cooked.ifName = s.interfaceName(cooked.ifIndex)
	switch cooked.packetType {
	case "outgoing":
		cooked.outputIf = cooked.ifIndex
		cooked.outputName = cooked.ifName
	case "loopback":
		cooked.inputIf = cooked.ifIndex
		cooked.outputIf = cooked.ifIndex
		cooked.inputName = cooked.ifName
		cooked.outputName = cooked.ifName
	default:
		cooked.inputIf = cooked.ifIndex
		cooked.inputName = cooked.ifName
	}
	return synthesizeEthernetFromCooked(data[20:], cooked.protocol, data[12:20]), cooked
}

func synthesizeEthernetFromCooked(payload []byte, protocol uint16, addr []byte) []byte {
	out := make([]byte, 14+len(payload))
	copy(out[6:12], cookedHardwareAddr(addr))
	binary.BigEndian.PutUint16(out[12:14], protocol)
	copy(out[14:], payload)
	return out
}

func cookedHardwareAddr(addr []byte) []byte {
	hw := make([]byte, 6)
	copy(hw, addr)
	return hw
}

func (s *Source) applyPcapMetadata(fields map[string]any, cooked cookedMetadata) {
	if cooked.linkType != 0 {
		fields["pcap_link_type"] = cooked.linkType
		fields["pcap_link_type_name"] = cooked.linkTypeName
	}
	if cooked.headerLength != 0 {
		fields["pcap_cooked_header_length"] = cooked.headerLength
		fields["linux_sll_protocol"] = uint32(cooked.protocol)
		fields["header_protocol"] = uint32(1)
		fields["protocol"] = uint32(1)
	}
	if cooked.packetType != "" {
		fields["capture_packet_type"] = cooked.packetType
	}
	if cooked.ifIndex != 0 {
		fields["linux_sll_ifindex"] = cooked.ifIndex
	}
	if cooked.inputIf != 0 {
		fields["input_if"] = cooked.inputIf
	}
	if cooked.outputIf != 0 {
		fields["output_if"] = cooked.outputIf
	}
	if cooked.inputName != "" {
		fields["input_interface"] = cooked.inputName
	}
	if cooked.outputName != "" {
		fields["output_interface"] = cooked.outputName
	}
}

func pcapLinkTypeName(linkType uint32) string {
	if linkType == linkTypeLinuxSLL2 {
		return "Linux SLL2"
	}
	if linkType <= 255 {
		return layers.LinkType(uint8(linkType)).String()
	}
	if name := pcap.DatalinkValToName(int(linkType)); name != "" {
		return name
	}
	return fmt.Sprintf("LinkType(%d)", linkType)
}

func linuxSLLPacketTypeName(packetType uint8) string {
	switch packetType {
	case 0:
		return "host"
	case 1:
		return "broadcast"
	case 2:
		return "multicast"
	case 3:
		return "otherhost"
	case 4:
		return "outgoing"
	case 5:
		return "loopback"
	case 6:
		return "fastroute"
	default:
		return fmt.Sprintf("unknown(%d)", packetType)
	}
}

func firstSystemIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].Index < ifaces[j].Index
	})
	for i := range ifaces {
		if ip := firstInterfaceIP(&ifaces[i]); ip != "127.0.0.1" {
			return ip
		}
	}
	return "127.0.0.1"
}

// firstInterfaceIP picks a stable agent IP for exported metadata, preferring
// non-loopback IPv4, then any non-loopback address, then localhost.
func firstInterfaceIP(iface *net.Interface) string {
	addrs, err := iface.Addrs()
	if err != nil {
		return "127.0.0.1"
	}
	var fallback string
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String()
		}
		if fallback == "" {
			fallback = ip.String()
		}
	}
	if fallback != "" {
		return fallback
	}
	return "127.0.0.1"
}
