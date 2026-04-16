package processor

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	flowmessage "github.com/netsampler/goflow2/v3/pb"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/packet"
)

// Processor converts source-specific payloads into ReFlow's in-memory event shape.
type Processor interface {
	Process(evt *event.Event) ([]*event.Event, error)
}

// New returns the configured processor implementation.
func New(cfg config.ProcessorConfig) (Processor, error) {
	switch cfg.Type {
	case "", "builtin":
		return NewBuiltin(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported processor.type %q", cfg.Type)
	}
}

type Builtin struct {
	cfg config.BuiltinProcessorConfig
}

// NewBuiltin constructs the default in-code processor used when WASM is disabled.
func NewBuiltin(cfg config.ProcessorConfig) *Builtin {
	return &Builtin{cfg: cfg.Builtin}
}

// Process dispatches to the built-in mapper for the incoming source message type.
func (p *Builtin) Process(evt *event.Event) ([]*event.Event, error) {
	if evt != nil && evt.Kind == "control" {
		return []*event.Event{evt}, nil
	}
	switch evt.Source.Type {
	case "bytes":
		return p.processBytes(evt)
	case "json":
		return p.processJSONFlavor(evt)
	case "flow":
		return p.processFlow(evt)
	default:
		return []*event.Event{evt}, nil
	}
}

// processBytes treats the payload as raw packet bytes and extracts the canonical
// L3/L4 tuple while preserving the raw bytes for later encoding if needed.
func (p *Builtin) processBytes(evt *event.Event) ([]*event.Event, error) {
	payload, ok := evt.Payload.([]byte)
	if !ok || len(payload) == 0 {
		return nil, fmt.Errorf("decode bytes packet: missing payload bytes")
	}

	fields := ensureFields(evt, 16)
	fields["message_type"] = "bytes"
	fields["record_kind"] = "packet"
	fields["frame_length"] = uint32(len(payload))
	fields["original_length"] = uint32(len(payload))
	fields["header_data"] = append([]byte(nil), payload...)
	fields["bytes"] = int64(len(payload))
	fields["packets"] = int64(1)
	if evt.ReceivedAt.IsZero() {
		evt.ReceivedAt = time.Now().UTC()
	}
	fields["start_time_unix"] = evt.ReceivedAt.UnixMilli()
	fields["end_time_unix"] = evt.ReceivedAt.UnixMilli()

	// sFlow raw packet headers expect protocol metadata in addition to the bytes.
	// ReFlow currently assumes Ethernet-framed capture for live pcap input.
	if _, ok := fields["protocol"]; !ok {
		fields["protocol"] = uint32(1)
	}
	if evt.SFlow == nil {
		evt.SFlow = &event.SFlowMetadata{}
	}
	if err := packet.NormalizeEvent(evt, packet.NormalizeOptions{
		DisablePacketMapping: p.cfg.DisablePacketMapping,
		BuildPseudoPacket:    p.cfg.BuildPseudoPacket,
		TruncatePacketBytes:  p.cfg.TruncatePacketBytes,
		UsePayloadAsPacket:   true,
		TruncatePayload:      true,
	}); err != nil {
		return nil, err
	}

	if p.cfg.DropMessage {
		evt.Message = nil
	}
	if p.cfg.DropPayload {
		evt.Payload = nil
	}
	return []*event.Event{evt}, nil
}

func (p *Builtin) setDefaultInterfaces(evt *event.Event, fields map[string]any) {
	if fieldUint32(fields, "input_if") != 0 || fieldUint32(fields, "output_if") != 0 {
		return
	}
	if evt.Source.CaptureInterfaceIndex <= 0 {
		return
	}
	ifIndex := uint32(evt.Source.CaptureInterfaceIndex)
	fields["input_if"] = ifIndex
	fields["output_if"] = ifIndex
}

// truncatePacketData limits large raw packet blobs while keeping derived tuple
// fields untouched.
func (p *Builtin) truncatePacketData(evt *event.Event, fields map[string]any) {
	maxBytes := p.cfg.TruncatePacketBytes
	if maxBytes <= 0 {
		return
	}
	headerData := bytesField(fields, "header_data")
	if len(headerData) > maxBytes {
		fields["header_data"] = append([]byte(nil), headerData[:maxBytes]...)
	}
	if payload, ok := evt.Payload.([]byte); ok && len(payload) > maxBytes {
		evt.Payload = append([]byte(nil), payload[:maxBytes]...)
	}
}

// bytesField reads either []byte or string data from the generic field map.
func bytesField(fields map[string]any, key string) []byte {
	if fields == nil {
		return nil
	}
	val, ok := fields[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		return nil
	}
}

// buildPseudoPacket synthesizes a minimal Ethernet+IP+L4 header from canonical
// flow fields so byte-oriented encoders still have packet material to work with.
func buildPseudoPacket(fields map[string]any) ([]byte, bool) {
	srcAddrStr := fieldStringOrZero(fields, "src_addr")
	dstAddrStr := fieldStringOrZero(fields, "dst_addr")
	if srcAddrStr == "" || dstAddrStr == "" {
		return nil, false
	}
	srcAddr, err := netip.ParseAddr(srcAddrStr)
	if err != nil {
		return nil, false
	}
	dstAddr, err := netip.ParseAddr(dstAddrStr)
	if err != nil {
		return nil, false
	}
	proto := fieldUint32(fields, "proto")
	srcPort := fieldUint32(fields, "src_port")
	dstPort := fieldUint32(fields, "dst_port")
	if srcAddr.Is4() && dstAddr.Is4() {
		return buildPseudoIPv4Packet(srcAddr, dstAddr, proto, srcPort, dstPort), true
	}
	if srcAddr.Is6() && dstAddr.Is6() {
		return buildPseudoIPv6Packet(srcAddr, dstAddr, proto, srcPort, dstPort), true
	}
	return nil, false
}

// buildPseudoIPv4Packet creates a minimal Ethernet/IPv4 packet with a stub TCP/UDP header.
func buildPseudoIPv4Packet(srcAddr, dstAddr netip.Addr, proto, srcPort, dstPort uint32) []byte {
	l4Len := pseudoL4HeaderLen(proto)
	packet := make([]byte, 14+20+l4Len)
	packet[12], packet[13] = 0x08, 0x00
	ip := packet[14:]
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(20+l4Len))
	ip[8] = 64
	ip[9] = byte(proto)
	src := srcAddr.As4()
	dst := dstAddr.As4()
	copy(ip[12:16], src[:])
	copy(ip[16:20], dst[:])
	fillPseudoL4(ip[20:], proto, srcPort, dstPort)
	return packet
}

// buildPseudoIPv6Packet creates a minimal Ethernet/IPv6 packet with a stub TCP/UDP header.
func buildPseudoIPv6Packet(srcAddr, dstAddr netip.Addr, proto, srcPort, dstPort uint32) []byte {
	l4Len := pseudoL4HeaderLen(proto)
	packet := make([]byte, 14+40+l4Len)
	packet[12], packet[13] = 0x86, 0xdd
	ip := packet[14:]
	ip[0] = 0x60
	binary.BigEndian.PutUint16(ip[4:6], uint16(l4Len))
	ip[6] = byte(proto)
	ip[7] = 64
	src := srcAddr.As16()
	dst := dstAddr.As16()
	copy(ip[8:24], src[:])
	copy(ip[24:40], dst[:])
	fillPseudoL4(ip[40:], proto, srcPort, dstPort)
	return packet
}

// pseudoL4HeaderLen returns the minimal header size needed for the selected protocol.
func pseudoL4HeaderLen(proto uint32) int {
	switch proto {
	case 6:
		return 20
	case 17:
		return 8
	default:
		return 8
	}
}

// fillPseudoL4 writes just enough transport-header structure for tuple-aware downstream use.
func fillPseudoL4(buf []byte, proto, srcPort, dstPort uint32) {
	if len(buf) < 8 {
		return
	}
	binary.BigEndian.PutUint16(buf[0:2], uint16(srcPort))
	binary.BigEndian.PutUint16(buf[2:4], uint16(dstPort))
	switch proto {
	case 6:
		if len(buf) < 20 {
			return
		}
		buf[12] = 0x50
	case 17:
		binary.BigEndian.PutUint16(buf[4:6], uint16(len(buf)))
	default:
		binary.BigEndian.PutUint16(buf[4:6], uint16(len(buf)))
	}
}

// mapPacketTuple parses header bytes back into canonical tuple fields when packet
// mapping is enabled.
func (p *Builtin) mapPacketTuple(fields map[string]any, packet []byte) {
	if p.cfg.DisablePacketMapping {
		return
	}
	if tuple, err := parsePacketTuple(packet); err == nil {
		fields["src_addr"] = tuple.SrcAddr.String()
		fields["dst_addr"] = tuple.DstAddr.String()
		fields["proto"] = tuple.Proto
		fields["proto_name"] = ipProtocolName(tuple.Proto)
		fields["src_port"] = tuple.SrcPort
		fields["dst_port"] = tuple.DstPort
	}
}

// ensurePseudoPacket backfills header_data when the event has flow fields but no
// raw packet bytes.
func (p *Builtin) ensurePseudoPacket(fields map[string]any) {
	if !p.cfg.BuildPseudoPacket {
		return
	}
	if len(bytesField(fields, "header_data")) > 0 {
		return
	}
	headerData, ok := buildPseudoPacket(fields)
	if !ok {
		return
	}
	fields["header_data"] = headerData
	if fieldUint32(fields, "frame_length") == 0 {
		fields["frame_length"] = uint32(len(headerData))
	}
	if fieldUint32(fields, "original_length") == 0 {
		fields["original_length"] = uint32(len(headerData))
	}
	if fieldUint32(fields, "protocol") == 0 {
		fields["protocol"] = uint32(1)
	}
}

// processFlow treats the built-in processor as the post-decode normalization boundary.
// The decode stage already expands exporter packets into canonical flow events, so the
// processor mainly validates the shape and optionally drops the raw datagram payload.
func (p *Builtin) processFlow(evt *event.Event) ([]*event.Event, error) {
	fields := ensureFields(evt, 2)
	if _, ok := fields["message_type"]; !ok {
		fields["message_type"] = "flow"
	}
	if _, ok := fields["flow_type"]; !ok {
		return nil, fmt.Errorf("decoded flow event is missing flow_type")
	}
	if len(bytesField(fields, "header_data")) > 0 || fieldStringOrZero(fields, "record_kind") == "packet" || (p.cfg.BuildPseudoPacket && hasPacketTuple(fields)) {
		if err := packet.NormalizeEvent(evt, packet.NormalizeOptions{
			DisablePacketMapping: p.cfg.DisablePacketMapping,
			BuildPseudoPacket:    p.cfg.BuildPseudoPacket,
			TruncatePacketBytes:  p.cfg.TruncatePacketBytes,
		}); err != nil {
			return nil, err
		}
	}
	if evt.SFlow == nil && (fieldStringOrZero(fields, "agent_ip") != "" || fieldUint32(fields, "sub_agent_id") != 0 || fieldUint32(fields, "source_id") != 0) {
		evt.SFlow = &event.SFlowMetadata{
			AgentIP:    fieldStringOrZero(fields, "agent_ip"),
			SubAgentID: fieldUint32(fields, "sub_agent_id"),
			SourceID:   fieldUint32(fields, "source_id"),
		}
	}
	if p.cfg.DropMessage {
		evt.Message = nil
	}
	if p.cfg.DropPayload {
		evt.Payload = nil
	}
	return []*event.Event{evt}, nil
}

func hasPacketTuple(fields map[string]any) bool {
	if fieldStringOrZero(fields, "src_addr") == "" || fieldStringOrZero(fields, "dst_addr") == "" {
		return false
	}
	return fieldUint32(fields, "proto") != 0
}

type rawPacketHeaderInput struct {
	AgentIP        string `json:"agent_ip"`
	SubAgentID     uint32 `json:"sub_agent_id"`
	SourceID       uint32 `json:"source_id"`
	SamplingRate   uint32 `json:"sampling_rate"`
	SamplePool     uint32 `json:"sample_pool"`
	Drops          uint32 `json:"drops"`
	InputIf        uint32 `json:"input_if"`
	OutputIf       uint32 `json:"output_if"`
	Protocol       uint32 `json:"protocol"`
	FrameLength    uint32 `json:"frame_length"`
	Stripped       uint32 `json:"stripped"`
	OriginalLength uint32 `json:"original_length"`
	HeaderHex      string `json:"header_hex"`
}

// processJSONRawPacketHeader accepts a user-friendly JSON representation of an
// sFlow sampled-header event and normalizes it into canonical packet fields.
func (p *Builtin) processJSONRawPacketHeader(evt *event.Event) ([]*event.Event, error) {
	var in rawPacketHeaderInput
	if err := json.Unmarshal(evt.Message, &in); err != nil {
		return nil, fmt.Errorf("decode json_raw_packet_header: %w", err)
	}

	headerData, err := hex.DecodeString(in.HeaderHex)
	if err != nil {
		return nil, fmt.Errorf("decode header_hex: %w", err)
	}

	if in.SamplingRate == 0 {
		in.SamplingRate = 1
	}
	if in.OriginalLength == 0 {
		in.OriginalLength = uint32(len(headerData))
	}
	if in.FrameLength == 0 {
		in.FrameLength = in.OriginalLength
	}

	fields := ensureFields(evt, 16)
	fields["agent_ip"] = in.AgentIP
	fields["sub_agent_id"] = in.SubAgentID
	fields["source_id"] = in.SourceID
	fields["sampling_rate"] = in.SamplingRate
	fields["sample_pool"] = in.SamplePool
	fields["drops"] = in.Drops
	fields["input_if"] = in.InputIf
	fields["output_if"] = in.OutputIf
	fields["protocol"] = in.Protocol
	fields["header_protocol_name"] = sampledHeaderProtocolName(in.Protocol)
	fields["frame_length"] = in.FrameLength
	fields["stripped"] = in.Stripped
	fields["original_length"] = in.OriginalLength
	fields["header_data"] = headerData
	fields["message_type"] = evt.Source.Type
	fields["bytes"] = int64(in.OriginalLength)
	fields["packets"] = int64(1)
	evt.SFlow = &event.SFlowMetadata{
		AgentIP:      in.AgentIP,
		SubAgentID:   in.SubAgentID,
		SourceID:     in.SourceID,
		SamplingRate: in.SamplingRate,
		SamplePool:   in.SamplePool,
		Drops:        in.Drops,
	}

	if err := packet.NormalizeEvent(evt, packet.NormalizeOptions{
		DisablePacketMapping: p.cfg.DisablePacketMapping,
		BuildPseudoPacket:    p.cfg.BuildPseudoPacket,
		TruncatePacketBytes:  p.cfg.TruncatePacketBytes,
	}); err != nil {
		return nil, err
	}

	if p.cfg.DropMessage {
		evt.Message = nil
	}

	return []*event.Event{evt}, nil
}

// processJSONFlavor maps generic JSON messages into canonical fields based on source metadata.
func (p *Builtin) processJSONFlavor(evt *event.Event) ([]*event.Event, error) {
	var payload any
	if err := json.Unmarshal(evt.Message, &payload); err != nil {
		return nil, fmt.Errorf("decode json flavor %q: %w", evt.Source.JSON.Flavor, err)
	}

	switch evt.Source.JSON.Flavor {
	case "reflow":
		return p.processReFlowJSON(evt, payload)
	case "raw_packet_header":
		record, ok := payload.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("raw_packet_header expects a JSON object")
		}
		data, err := json.Marshal(record)
		if err != nil {
			return nil, fmt.Errorf("encode raw_packet_header payload: %w", err)
		}
		evt.Message = data
		return p.processJSONRawPacketHeader(evt)
	case "vendor":
		return p.processVendor(evt, payload)
	case "goflow2v2":
		return p.processGoFlow2V2(evt, payload)
	default:
		return nil, fmt.Errorf("unsupported source.json.flavor %q", evt.Source.JSON.Flavor)
	}
}

// processVendor keeps opaque vendor JSON intact while tagging it so routing and
// encoding can treat it as a distinct message class.
func (p *Builtin) processVendor(evt *event.Event, payload any) ([]*event.Event, error) {
	_, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("vendor expects a JSON object")
	}

	fields := ensureFields(evt, 2)
	fields["message_type"] = "vendor"
	fields["json_flavor"] = evt.Source.JSON.Flavor
	if p.cfg.DropMessage {
		evt.Message = nil
	}
	return []*event.Event{evt}, nil
}

// processReFlowJSON accepts the native ReFlow JSON shape and forwards field
// extraction to processReFlowFields.
func (p *Builtin) processReFlowJSON(evt *event.Event, payload any) ([]*event.Event, error) {
	record, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reflow expects a JSON object")
	}
	return p.processReFlowFields(evt, record), nil
}

// processReFlowFields copies a native ReFlow JSON object directly into the event field map.
func (p *Builtin) processReFlowFields(evt *event.Event, record map[string]any) []*event.Event {
	fields := ensureFields(evt, len(record))
	for key, value := range record {
		fields[key] = value
	}
	if evt.SFlow == nil && (fieldStringOrZero(fields, "agent_ip") != "" || fieldUint32(fields, "sub_agent_id") != 0 || fieldUint32(fields, "source_id") != 0) {
		evt.SFlow = &event.SFlowMetadata{
			AgentIP:    fieldStringOrZero(fields, "agent_ip"),
			SubAgentID: fieldUint32(fields, "sub_agent_id"),
			SourceID:   fieldUint32(fields, "source_id"),
		}
	}
	if p.cfg.DropMessage {
		evt.Message = nil
	}
	return []*event.Event{evt}
}

// processGoFlow2V2 translates the existing goflow2 JSON schema into ReFlow's
// canonical field set so users can bridge between the old and new binaries.
func (p *Builtin) processGoFlow2V2(evt *event.Event, payload any) ([]*event.Event, error) {
	record, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("goflow2v2 expects a JSON object")
	}

	fields := ensureFields(evt, 24)
	if ip := decodeMaybeBase64IP(stringAlias(record, "sampler_address")); ip != "" {
		fields["agent_ip"] = ip
	}
	if ip := decodeMaybeBase64IP(stringAlias(record, "src_addr")); ip != "" {
		fields["src_addr"] = ip
	}
	if ip := decodeMaybeBase64IP(stringAlias(record, "dst_addr")); ip != "" {
		fields["dst_addr"] = ip
	}
	setUint32Alias(fields, record, "src_port", "src_port")
	setUint32Alias(fields, record, "dst_port", "dst_port")
	setUint32Alias(fields, record, "proto", "proto")
	if proto := fieldUint32(fields, "proto"); proto != 0 {
		fields["proto_name"] = ipProtocolName(proto)
	}
	setUint32Alias(fields, record, "sampling_rate", "sampling_rate")
	setUint32Alias(fields, record, "input_if", "in_if")
	setUint32Alias(fields, record, "output_if", "out_if")
	setInt64Alias(fields, record, "bytes", "bytes")
	setInt64Alias(fields, record, "packets", "packets")
	setTimeNSAlias(fields, record, "start_time_unix", "time_flow_start_ns")
	setTimeNSAlias(fields, record, "end_time_unix", "time_flow_end_ns")
	fields["message_type"] = "goflow2v2"
	fields["json_flavor"] = evt.Source.JSON.Flavor

	if typeVal, ok := record["type"]; ok {
		switch flowType := uint32FromAny(typeVal); flowType {
		case uint32(flowmessage.FlowMessage_SFLOW_5):
			fields["flow_type"] = "sflow"
		case uint32(flowmessage.FlowMessage_NETFLOW_V5):
			fields["flow_type"] = "netflowv5"
		case uint32(flowmessage.FlowMessage_NETFLOW_V9):
			fields["flow_type"] = "netflowv9"
		case uint32(flowmessage.FlowMessage_IPFIX):
			fields["flow_type"] = "ipfix"
		default:
			fields["flow_type"] = flowType
		}
	}
	evt.SFlow = &event.SFlowMetadata{
		AgentIP:      fieldStringOrZero(fields, "agent_ip"),
		SubAgentID:   fieldUint32(fields, "sub_agent_id"),
		SourceID:     fieldUint32(fields, "source_id"),
		SamplingRate: fieldUint32(fields, "sampling_rate"),
		SamplePool:   fieldUint32(fields, "sample_pool"),
		Drops:        fieldUint32(fields, "drops"),
	}
	p.ensurePseudoPacket(fields)

	if p.cfg.DropMessage {
		evt.Message = nil
	}
	return []*event.Event{evt}, nil
}

type packetTuple struct {
	SrcAddr netip.Addr
	DstAddr netip.Addr
	Proto   uint32
	SrcPort uint32
	DstPort uint32
}

// parsePacketTuple extracts a minimal L3/L4 tuple from a sampled raw packet header.
func parsePacketTuple(data []byte) (packetTuple, error) {
	if len(data) == 0 {
		return packetTuple{}, fmt.Errorf("empty packet header")
	}
	offset := 0
	if len(data) >= 14 {
		etherType := uint16(data[12])<<8 | uint16(data[13])
		if etherType == 0x0800 || etherType == 0x86dd {
			offset = 14
		}
	}
	if len(data) <= offset {
		return packetTuple{}, fmt.Errorf("truncated packet header")
	}
	switch data[offset] >> 4 {
	case 4:
		return parseIPv4Tuple(data[offset:])
	case 6:
		return parseIPv6Tuple(data[offset:])
	default:
		return packetTuple{}, fmt.Errorf("unsupported ip version")
	}
}

func parseIPv4Tuple(data []byte) (packetTuple, error) {
	if len(data) < 20 {
		return packetTuple{}, fmt.Errorf("truncated ipv4 header")
	}
	ihl := int(data[0]&0x0f) * 4
	if ihl < 20 || len(data) < ihl {
		return packetTuple{}, fmt.Errorf("invalid ipv4 header length")
	}
	src, ok := netip.AddrFromSlice(data[12:16])
	if !ok {
		return packetTuple{}, fmt.Errorf("invalid ipv4 source address")
	}
	dst, ok := netip.AddrFromSlice(data[16:20])
	if !ok {
		return packetTuple{}, fmt.Errorf("invalid ipv4 destination address")
	}
	tuple := packetTuple{
		SrcAddr: src,
		DstAddr: dst,
		Proto:   uint32(data[9]),
	}
	if len(data) >= ihl+4 && (tuple.Proto == 6 || tuple.Proto == 17) {
		tuple.SrcPort = uint32(uint16(data[ihl])<<8 | uint16(data[ihl+1]))
		tuple.DstPort = uint32(uint16(data[ihl+2])<<8 | uint16(data[ihl+3]))
	}
	return tuple, nil
}

// parseIPv6Tuple walks past common extension headers before reading transport ports.
func parseIPv6Tuple(data []byte) (packetTuple, error) {
	if len(data) < 40 {
		return packetTuple{}, fmt.Errorf("truncated ipv6 header")
	}
	src, ok := netip.AddrFromSlice(data[8:24])
	if !ok {
		return packetTuple{}, fmt.Errorf("invalid ipv6 source address")
	}
	dst, ok := netip.AddrFromSlice(data[24:40])
	if !ok {
		return packetTuple{}, fmt.Errorf("invalid ipv6 destination address")
	}
	nextHeader := data[6]
	offset := 40
	for {
		if !isIPv6ExtensionHeader(nextHeader) {
			break
		}
		if len(data) < offset+2 {
			return packetTuple{}, fmt.Errorf("truncated ipv6 extension header")
		}
		switch nextHeader {
		case 44:
			if len(data) < offset+8 {
				return packetTuple{}, fmt.Errorf("truncated ipv6 fragment header")
			}
			nextHeader = data[offset]
			offset += 8
		case 51:
			hdrLen := (int(data[offset+1]) + 2) * 4
			if len(data) < offset+hdrLen {
				return packetTuple{}, fmt.Errorf("truncated ipv6 authentication header")
			}
			nextHeader = data[offset]
			offset += hdrLen
		default:
			hdrLen := (int(data[offset+1]) + 1) * 8
			if len(data) < offset+hdrLen {
				return packetTuple{}, fmt.Errorf("truncated ipv6 extension header")
			}
			nextHeader = data[offset]
			offset += hdrLen
		}
	}
	tuple := packetTuple{
		SrcAddr: src,
		DstAddr: dst,
		Proto:   uint32(nextHeader),
	}
	if len(data) >= offset+4 && (tuple.Proto == 6 || tuple.Proto == 17) {
		tuple.SrcPort = uint32(uint16(data[offset])<<8 | uint16(data[offset+1]))
		tuple.DstPort = uint32(uint16(data[offset+2])<<8 | uint16(data[offset+3]))
	}
	return tuple, nil
}

// isIPv6ExtensionHeader lists the IPv6 next-header values that need header walking.
func isIPv6ExtensionHeader(nextHeader byte) bool {
	switch nextHeader {
	case 0, 43, 44, 50, 51, 60, 135, 139, 140:
		return true
	default:
		return false
	}
}

// ensureFields lazily allocates the processor field map.
func ensureFields(evt *event.Event, capacity int) map[string]any {
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, capacity)
	}
	return evt.Fields
}

// fieldStringOrZero normalizes string-like field values from the generic map.
func fieldStringOrZero(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	val, ok := fields[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

// fieldUint32 reads one field from the generic field map and normalizes it to u32.
func fieldUint32(fields map[string]any, key string) uint32 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	return uint32FromAny(val)
}

// stringAlias returns the first present source key as a string.
func stringAlias(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if val, ok := m[key]; ok {
			switch v := val.(type) {
			case string:
				return v
			case fmt.Stringer:
				return v.String()
			default:
				return fmt.Sprint(v)
			}
		}
	}
	return ""
}

// setUint32Alias copies the first matching source key into the destination under dstKey.
func setUint32Alias(dst map[string]any, src map[string]any, dstKey string, srcKeys ...string) {
	if src == nil {
		return
	}
	for _, key := range srcKeys {
		if val, ok := src[key]; ok {
			dst[dstKey] = uint32FromAny(val)
			return
		}
	}
}

// setInt64Alias copies one numeric value from any of the alias keys.
func setInt64Alias(dst map[string]any, src map[string]any, dstKey string, srcKeys ...string) {
	if src == nil {
		return
	}
	for _, key := range srcKeys {
		if val, ok := src[key]; ok {
			dst[dstKey] = int64FromAny(val)
			return
		}
	}
}

// setTimeNSAlias converts nanosecond timestamps into the millisecond unit used by ReFlow fields.
func setTimeNSAlias(dst map[string]any, src map[string]any, dstKey string, srcKeys ...string) {
	if src == nil {
		return
	}
	for _, key := range srcKeys {
		if val, ok := src[key]; ok {
			ns := int64FromAny(val)
			if ns == 0 {
				return
			}
			dst[dstKey] = ns / int64(time.Millisecond)
			return
		}
	}
}

// uint32FromAny normalizes the common JSON and generic-map number representations.
func uint32FromAny(val any) uint32 {
	switch v := val.(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case int:
		return uint32(v)
	case int64:
		return uint32(v)
	case float64:
		return uint32(v)
	case string:
		return uint32(int64FromString(v))
	default:
		return 0
	}
}

// int64FromAny normalizes the common JSON and generic-map number representations.
func int64FromAny(val any) int64 {
	switch v := val.(type) {
	case int64:
		return v
	case uint64:
		return int64(v)
	case uint32:
		return int64(v)
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		return int64FromString(v)
	default:
		return 0
	}
}

// int64FromString parses a decimal string and returns zero on failure.
func int64FromString(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// decodeMaybeBase64IP accepts either a normal IP string or the base64-encoded
// byte form used by legacy goflow2 JSON.
func decodeMaybeBase64IP(val string) string {
	if val == "" {
		return ""
	}
	if addr, err := netip.ParseAddr(val); err == nil {
		return addr.String()
	}
	raw, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		return ""
	}
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return ""
	}
	return addr.String()
}

// ipProtocolName maps common IANA protocol numbers to readable names.
func ipProtocolName(proto uint32) string {
	switch proto {
	case 1:
		return "icmp"
	case 2:
		return "igmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 41:
		return "ipv6"
	case 47:
		return "gre"
	case 50:
		return "esp"
	case 51:
		return "ah"
	case 58:
		return "icmpv6"
	case 132:
		return "sctp"
	default:
		return ""
	}
}

// sampledHeaderProtocolName maps sFlow sampled-header protocol IDs to names.
func sampledHeaderProtocolName(proto uint32) string {
	switch proto {
	case 1:
		return "ethernet"
	case 11:
		return "ipv4"
	case 12:
		return "ipv6"
	default:
		return ""
	}
}
