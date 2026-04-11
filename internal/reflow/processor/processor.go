package processor

import (
	"encoding/base64"
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
	switch evt.Source.Type {
	case "bytes":
		return nil, fmt.Errorf("builtin processor does not support source.type=bytes; use a custom WASM processor")
	case "json":
		return p.processJSONFlavor(evt)
	case "flow":
		return p.processFlow(evt)
	default:
		return []*event.Event{evt}, nil
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
	if p.cfg.DropMessage {
		evt.Message = nil
	}
	if p.cfg.DropPayload {
		evt.Payload = nil
	}
	return []*event.Event{evt}, nil
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
	fields["frame_length"] = in.FrameLength
	fields["stripped"] = in.Stripped
	fields["original_length"] = in.OriginalLength
	fields["header_data"] = headerData
	fields["message_type"] = evt.Source.Type
	fields["bytes"] = int64(in.OriginalLength)
	fields["packets"] = int64(1)

	if tuple, err := parsePacketTuple(headerData); err == nil {
		fields["src_addr"] = tuple.SrcAddr.String()
		fields["dst_addr"] = tuple.DstAddr.String()
		fields["proto"] = tuple.Proto
		fields["src_port"] = tuple.SrcPort
		fields["dst_port"] = tuple.DstPort
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
	case "reflow", "raw_packet_header":
		return p.processReFlowJSON(evt, payload)
	case "vendor":
		return p.processVendor(evt, payload)
	case "goflow2v2":
		return p.processGoFlow2V2(evt, payload)
	default:
		return nil, fmt.Errorf("unsupported source.json.flavor %q", evt.Source.JSON.Flavor)
	}
}

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

func (p *Builtin) processReFlowJSON(evt *event.Event, payload any) ([]*event.Event, error) {
	record, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reflow expects a JSON object")
	}

	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode reflow payload: %w", err)
	}
	evt.Message = data
	return p.processJSONRawPacketHeader(evt)
}

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
	tuple := packetTuple{
		SrcAddr: src,
		DstAddr: dst,
		Proto:   uint32(data[6]),
	}
	if len(data) >= 44 && (tuple.Proto == 6 || tuple.Proto == 17) {
		tuple.SrcPort = uint32(uint16(data[40])<<8 | uint16(data[41]))
		tuple.DstPort = uint32(uint16(data[42])<<8 | uint16(data[43]))
	}
	return tuple, nil
}

func ensureFields(evt *event.Event, capacity int) map[string]any {
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, capacity)
	}
	return evt.Fields
}

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

func int64FromString(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

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
