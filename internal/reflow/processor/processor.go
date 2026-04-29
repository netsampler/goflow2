package processor

import (
	"bytes"
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
	cfg    config.BuiltinProcessorConfig
	decode packet.DecodeOptions
}

// NewBuiltin constructs the default in-code processor used when WASM is disabled.
func NewBuiltin(cfg config.ProcessorConfig) *Builtin {
	return &Builtin{
		cfg:    cfg.Builtin,
		decode: packetDecodeOptions(cfg.Builtin.PacketDecoder),
	}
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
	fields["record_kind"] = "packet"
	fields["frame_length"] = frameLength
	fields["original_length"] = uint32(len(payload))
	fields["bytes"] = int64(frameLength)
	fields["packets"] = int64(1)
	if evt.ReceivedAt.IsZero() {
		evt.ReceivedAt = time.Now().UTC()
	}
	startNS, endNS := packetTimeWindowNS(evt.ReceivedAt, frameLength, fieldUint64(fields, "if_speed"))
	fields["time_flow_start_ns"] = startNS
	fields["time_flow_end_ns"] = endNS
	// Aggregators use the existing millisecond fields for bucket windows and
	// min/max merging; nanosecond aliases above preserve packet timing precision.
	fields["start_time_unix"] = startNS / int64(time.Millisecond)
	fields["end_time_unix"] = endNS / int64(time.Millisecond)

	// sFlow raw packet headers expect protocol metadata in addition to the bytes.
	// Live pcap defaults to Ethernet, while stream sources can provide a header_protocol.
	if _, ok := fields["protocol"]; !ok {
		fields["protocol"] = uint32(1)
	}
	if err := packet.NormalizeEvent(evt, packet.NormalizeOptions{
		DisablePacketMapping: p.cfg.DisablePacketMapping,
		TruncatePacketBytes:  p.cfg.TruncatePacketBytes,
		UsePayloadAsPacket:   true,
		TruncatePayload:      true,
		HeaderProtocol:       fieldUint32(fields, "header_protocol"),
		Decode:               p.decode,
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
	if _, ok := fields["flow_type"]; !ok {
		return nil, fmt.Errorf("decoded flow event is missing flow_type")
	}
	if len(bytesField(fields, "header_data")) > 0 || fieldStringOrZero(fields, "record_kind") == "packet" {
		if err := packet.NormalizeEvent(evt, packet.NormalizeOptions{
			DisablePacketMapping: p.cfg.DisablePacketMapping,
			TruncatePacketBytes:  p.cfg.TruncatePacketBytes,
			HeaderProtocol:       packetHeaderProtocol(fields),
			Decode:               p.decode,
		}); err != nil {
			return nil, err
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
	fields["bytes"] = int64(in.FrameLength)
	fields["packets"] = int64(1)
	if err := packet.NormalizeEvent(evt, packet.NormalizeOptions{
		DisablePacketMapping: p.cfg.DisablePacketMapping,
		TruncatePacketBytes:  p.cfg.TruncatePacketBytes,
		HeaderProtocol:       in.Protocol,
		Decode:               p.decode,
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
	decoder := json.NewDecoder(bytes.NewReader(evt.Message))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
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
	if fields, ok := canonicalEventFields(record); ok {
		return p.processReFlowFields(evt, fields)
	}
	return p.processReFlowFields(evt, record)
}

// processReFlowFields copies a native ReFlow JSON object directly into the event field map.
func (p *Builtin) processReFlowFields(evt *event.Event, record map[string]any) ([]*event.Event, error) {
	fields := ensureFields(evt, len(record))
	for key, value := range record {
		if key == "packet" {
			model, err := reFlowPacketModelFromValue(value)
			if err != nil {
				return nil, err
			}
			evt.Packet = model
			packet.ApplyModelFields(fields, model)
			continue
		}
		fields[key] = normalizeReFlowJSONValue(key, value)
	}
	if p.cfg.DropMessage {
		evt.Message = nil
	}
	return []*event.Event{evt}, nil
}

func reFlowPacketModelFromValue(value any) (*event.PacketModel, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode reflow packet model: %w", err)
	}
	var model event.PacketModel
	if err := json.Unmarshal(data, &model); err != nil {
		return nil, fmt.Errorf("decode reflow packet model: %w", err)
	}
	return &model, nil
}

func canonicalEventFields(record map[string]any) (map[string]any, bool) {
	fields, ok := record["fields"].(map[string]any)
	if !ok {
		return nil, false
	}
	for _, key := range []string{"received_at", "kind", "stream", "source", "control", "message", "packet", "sflow"} {
		if _, ok := record[key]; ok {
			return fields, true
		}
	}
	return nil, false
}

func normalizeReFlowJSONValue(key string, value any) any {
	switch typed := value.(type) {
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return n
		}
		f, err := typed.Float64()
		if err != nil {
			return value
		}
		return f
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeReFlowJSONValue("", item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, item := range typed {
			out[k] = normalizeReFlowJSONValue(k, item)
		}
		return out
	default:
		return value
	}
}

func packetDecodeOptions(cfg config.PacketDecoderConfig) packet.DecodeOptions {
	opts := packet.DecodeOptions{
		Configured:     true,
		DecodeBeyondL4: true,
		DecodeGRE:      true,
		DecodeIPIP:     true,
		DecodeIP6IP:    true,
		DecodeVXLAN:    true,
		DecodeGeneve:   true,
		DecodeL2TP:     true,
		DecodeGTPU:     true,
		DecodePPPoE:    true,
	}
	if cfg.DecodeBeyondL4 != nil {
		opts.DecodeBeyondL4 = *cfg.DecodeBeyondL4
	}
	encaps := cfg.Encapsulations
	if encaps.GRE.Enabled != nil {
		opts.DecodeGRE = *encaps.GRE.Enabled
	}
	if len(encaps.GRE.Protocols) > 0 {
		opts.GREProtocols = append([]uint32(nil), encaps.GRE.Protocols...)
	}
	if encaps.IPIP.Enabled != nil {
		opts.DecodeIPIP = *encaps.IPIP.Enabled
	}
	if len(encaps.IPIP.Protocols) > 0 {
		opts.IPIPProtocols = append([]uint32(nil), encaps.IPIP.Protocols...)
	}
	if encaps.IP6IP.Enabled != nil {
		opts.DecodeIP6IP = *encaps.IP6IP.Enabled
	}
	if len(encaps.IP6IP.Protocols) > 0 {
		opts.IP6IPProtocols = append([]uint32(nil), encaps.IP6IP.Protocols...)
	}
	if encaps.VXLAN.Enabled != nil {
		opts.DecodeVXLAN = *encaps.VXLAN.Enabled
	}
	if len(encaps.VXLAN.Ports) > 0 {
		opts.VXLANPorts = append([]uint32(nil), encaps.VXLAN.Ports...)
	}
	if encaps.Geneve.Enabled != nil {
		opts.DecodeGeneve = *encaps.Geneve.Enabled
	}
	if len(encaps.Geneve.Ports) > 0 {
		opts.GenevePorts = append([]uint32(nil), encaps.Geneve.Ports...)
	}
	if encaps.L2TP.Enabled != nil {
		opts.DecodeL2TP = *encaps.L2TP.Enabled
	}
	if len(encaps.L2TP.Ports) > 0 {
		opts.L2TPPorts = append([]uint32(nil), encaps.L2TP.Ports...)
	}
	if encaps.GTPU.Enabled != nil {
		opts.DecodeGTPU = *encaps.GTPU.Enabled
	}
	if len(encaps.GTPU.Ports) > 0 {
		opts.GTPUPorts = append([]uint32(nil), encaps.GTPU.Ports...)
	}
	if encaps.PPPoE.Enabled != nil {
		opts.DecodePPPoE = *encaps.PPPoE.Enabled
	}
	return opts
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

// fieldUint64 reads one field from the generic field map and normalizes it to u64.
func fieldUint64(fields map[string]any, key string) uint64 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	return uint64FromAny(val)
}

// packetTimeWindowNS preserves nanosecond timing for raw packet events. When an
// interface speed is available, the end time includes the estimated wire time;
// otherwise a sampled packet is treated as an instant.
func packetTimeWindowNS(start time.Time, frameLength uint32, ifSpeed uint64) (int64, int64) {
	startNS := start.UnixNano()
	durationNS := packetTransmissionDurationNS(frameLength, ifSpeed)
	const maxInt64 = int64(1<<63 - 1)
	if durationNS > 0 && startNS > maxInt64-durationNS {
		return startNS, maxInt64
	}
	return startNS, startNS + durationNS
}

func packetTransmissionDurationNS(frameLength uint32, ifSpeed uint64) int64 {
	if frameLength == 0 || ifSpeed == 0 {
		return 0
	}
	const maxInt64 = uint64(1<<63 - 1)
	bits := uint64(frameLength) * 8
	if bits > maxInt64/uint64(time.Second) {
		return int64(maxInt64)
	}
	bitNanos := bits * uint64(time.Second)
	durationNS := bitNanos / ifSpeed
	if bitNanos%ifSpeed != 0 {
		durationNS++
	}
	if durationNS > maxInt64 {
		return int64(maxInt64)
	}
	return int64(durationNS)
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
			switch dstKey {
			case "start_time_unix":
				dst["time_flow_start_ns"] = ns
			case "end_time_unix":
				dst["time_flow_end_ns"] = ns
			}
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
		if v < 0 {
			return 0
		}
		return uint32(v)
	case int64:
		if v < 0 {
			return 0
		}
		return uint32(v)
	case float64:
		if v < 0 {
			return 0
		}
		return uint32(v)
	case json.Number:
		n, _ := v.Int64()
		if n < 0 {
			return 0
		}
		return uint32(n)
	case string:
		n := int64FromString(v)
		if n < 0 {
			return 0
		}
		return uint32(n)
	default:
		return 0
	}
}

// uint64FromAny normalizes the common JSON and generic-map number representations.
func uint64FromAny(val any) uint64 {
	switch v := val.(type) {
	case uint64:
		return v
	case uint32:
		return uint64(v)
	case int:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case int64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case float64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case json.Number:
		n, _ := v.Int64()
		if n < 0 {
			return 0
		}
		return uint64(n)
	case string:
		n := int64FromString(v)
		if n < 0 {
			return 0
		}
		return uint64(n)
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
	case json.Number:
		n, _ := v.Int64()
		return n
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
