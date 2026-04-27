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
	frameLength := uint32(len(payload))
	if wireLength := fieldUint32(fields, "wire_length"); wireLength != 0 {
		frameLength = wireLength
	}
	fields["message_type"] = "bytes"
	fields["record_kind"] = "packet"
	fields["frame_length"] = frameLength
	fields["original_length"] = uint32(len(payload))
	fields["header_data"] = append([]byte(nil), payload...)
	fields["bytes"] = int64(frameLength)
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
			HeaderProtocol:       packetHeaderProtocol(fields),
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

func packetHeaderProtocol(fields map[string]any) uint32 {
	if fieldStringOrZero(fields, "flow_type") != "sflow" || fieldStringOrZero(fields, "record_kind") != "packet" {
		return 0
	}
	return fieldUint32(fields, "protocol")
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
	fields["bytes"] = int64(in.FrameLength)
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
		HeaderProtocol:       in.Protocol,
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
	if p.cfg.BuildPseudoPacket && hasPacketTuple(fields) {
		if err := packet.NormalizeEvent(evt, packet.NormalizeOptions{
			DisablePacketMapping: p.cfg.DisablePacketMapping,
			BuildPseudoPacket:    p.cfg.BuildPseudoPacket,
			TruncatePacketBytes:  p.cfg.TruncatePacketBytes,
		}); err != nil {
			return nil, err
		}
	}

	if p.cfg.DropMessage {
		evt.Message = nil
	}
	return []*event.Event{evt}, nil
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
