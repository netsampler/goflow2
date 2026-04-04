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
	case "json":
		return p.processJSONFlavor(evt)
	default:
		return []*event.Event{evt}, nil
	}
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
	case "vpc_flow_logs", "aws_vpc_flow_logs":
		return p.processVPCFlowLogs(evt, payload)
	case "azure_flow_logs", "azure_nsg_flow_logs":
		return p.processAzureFlowLogs(evt, payload)
	case "google_flow_logs", "gcp_vpc_flow_logs":
		return p.processGoogleFlowLogs(evt, payload)
	case "goflow2v2":
		return p.processGoFlow2V2(evt, payload)
	default:
		return nil, fmt.Errorf("unsupported source.json.flavor %q", evt.Source.JSON.Flavor)
	}
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

func (p *Builtin) processVPCFlowLogs(evt *event.Event, payload any) ([]*event.Event, error) {
	record, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("vpc_flow_logs expects a JSON object")
	}

	fields := ensureFields(evt, 24)
	setStringAlias(fields, record, "src_addr", "srcaddr", "src_addr")
	setStringAlias(fields, record, "dst_addr", "dstaddr", "dst_addr")
	setUint32Alias(fields, record, "src_port", "srcport", "src_port")
	setUint32Alias(fields, record, "dst_port", "dstport", "dst_port")
	setUint32Alias(fields, record, "proto", "protocol", "proto")
	setInt64Alias(fields, record, "packets", "packets")
	setInt64Alias(fields, record, "bytes", "bytes")
	setStringAlias(fields, record, "action", "action")
	setStringAlias(fields, record, "log_status", "log_status", "logstatus")
	copyAlias(fields, record, "account_id", "account_id", "account-id")
	copyAlias(fields, record, "interface_id", "interface_id", "interface-id")
	copyAlias(fields, record, "instance_id", "instance_id", "instance-id")
	copyAlias(fields, record, "vpc_id", "vpc_id", "vpc-id")
	copyAlias(fields, record, "subnet_id", "subnet_id", "subnet-id")
	copyAlias(fields, record, "region", "region")
	setTimeAlias(fields, record, "start_time_unix", "start", "start_time")
	setTimeAlias(fields, record, "end_time_unix", "end", "end_time")
	fields["json_flavor"] = evt.Source.JSON.Flavor

	if p.cfg.DropMessage {
		evt.Message = nil
	}
	return []*event.Event{evt}, nil
}

func (p *Builtin) processAzureFlowLogs(evt *event.Event, payload any) ([]*event.Event, error) {
	root, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("azure_flow_logs expects a JSON object")
	}

	events := make([]*event.Event, 0, 8)
	if tuples := collectAzureTuples(root); len(tuples) > 0 {
		for _, tuple := range tuples {
			item := cloneEvent(evt)
			fields := ensureFields(item, 20)
			fields["src_addr"] = tuple.SrcAddr
			fields["dst_addr"] = tuple.DstAddr
			fields["src_port"] = tuple.SrcPort
			fields["dst_port"] = tuple.DstPort
			fields["proto"] = tuple.Proto
			fields["packets"] = tuple.Packets
			fields["bytes"] = tuple.Bytes
			fields["flow_direction"] = tuple.Direction
			fields["traffic_decision"] = tuple.Decision
			fields["flow_state"] = tuple.State
			fields["start_time_unix"] = tuple.StartUnix
			fields["json_flavor"] = evt.Source.JSON.Flavor
			if p.cfg.DropMessage {
				item.Message = nil
			}
			events = append(events, item)
		}
		return events, nil
	}

	fields := ensureFields(evt, 20)
	setStringAlias(fields, root, "src_addr", "src_ip", "srcaddr", "srcAddr")
	setStringAlias(fields, root, "dst_addr", "dest_ip", "dst_ip", "dstaddr", "dstAddr")
	setUint32Alias(fields, root, "src_port", "src_port", "source_port", "srcPort")
	setUint32Alias(fields, root, "dst_port", "dest_port", "dst_port", "destination_port", "dstPort")
	setUint32Alias(fields, root, "proto", "protocol", "proto")
	setInt64Alias(fields, root, "packets", "packets")
	setInt64Alias(fields, root, "bytes", "bytes")
	setStringAlias(fields, root, "flow_direction", "flow_direction", "traffic_flow")
	setStringAlias(fields, root, "traffic_decision", "traffic_decision", "decision")
	fields["json_flavor"] = evt.Source.JSON.Flavor

	if p.cfg.DropMessage {
		evt.Message = nil
	}
	return []*event.Event{evt}, nil
}

func (p *Builtin) processGoogleFlowLogs(evt *event.Event, payload any) ([]*event.Event, error) {
	record, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("google_flow_logs expects a JSON object")
	}

	fields := ensureFields(evt, 24)
	connection := objectAt(record, "connection")
	setStringAlias(fields, connection, "src_addr", "src_ip", "srcIp")
	setStringAlias(fields, connection, "dst_addr", "dest_ip", "destIp")
	setUint32Alias(fields, connection, "src_port", "src_port", "srcPort")
	setUint32Alias(fields, connection, "dst_port", "dest_port", "destPort")
	setUint32Alias(fields, connection, "proto", "protocol")

	if len(fields) == 0 || fields["src_addr"] == nil {
		setStringAlias(fields, record, "src_addr", "src_ip", "srcIp")
		setStringAlias(fields, record, "dst_addr", "dest_ip", "dst_ip", "destIp")
		setUint32Alias(fields, record, "src_port", "src_port", "srcPort")
		setUint32Alias(fields, record, "dst_port", "dest_port", "dst_port", "destPort")
		setUint32Alias(fields, record, "proto", "protocol")
	}

	setInt64Alias(fields, record, "bytes", "bytes_sent", "bytes")
	setInt64Alias(fields, record, "packets", "packets_sent", "packets")
	setStringAlias(fields, record, "reporter", "reporter")
	setStringAlias(fields, record, "disposition", "disposition")
	setTimeAlias(fields, record, "start_time_unix", "start_time")
	setTimeAlias(fields, record, "end_time_unix", "end_time")
	fields["json_flavor"] = evt.Source.JSON.Flavor

	if p.cfg.DropMessage {
		evt.Message = nil
	}
	return []*event.Event{evt}, nil
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

type azureTuple struct {
	StartUnix int64
	SrcAddr   string
	DstAddr   string
	SrcPort   uint32
	DstPort   uint32
	Proto     uint32
	Direction string
	Decision  string
	State     string
	Packets   int64
	Bytes     int64
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

func cloneEvent(evt *event.Event) *event.Event {
	item := &event.Event{
		ReceivedAt: evt.ReceivedAt,
		Source:     evt.Source,
		Message:    evt.Message,
	}
	if evt.Fields != nil {
		item.Fields = make(map[string]any, len(evt.Fields))
		for k, v := range evt.Fields {
			item.Fields[k] = v
		}
	}
	return item
}

func collectAzureTuples(root map[string]any) []azureTuple {
	var out []azureTuple
	walkAny(root, func(key string, value any) {
		if key != "flowTuples" {
			return
		}
		items, ok := value.([]any)
		if !ok {
			return
		}
		for _, item := range items {
			s, ok := item.(string)
			if !ok {
				continue
			}
			if tuple, ok := parseAzureTuple(s); ok {
				out = append(out, tuple)
			}
		}
	})
	return out
}

func walkAny(val any, visit func(key string, value any)) {
	switch v := val.(type) {
	case map[string]any:
		for key, item := range v {
			visit(key, item)
			walkAny(item, visit)
		}
	case []any:
		for _, item := range v {
			walkAny(item, visit)
		}
	}
}

func parseAzureTuple(s string) (azureTuple, bool) {
	parts := strings.Split(s, ",")
	if len(parts) < 8 {
		return azureTuple{}, false
	}
	out := azureTuple{
		StartUnix: int64FromString(parts[0]),
		SrcAddr:   parts[1],
		DstAddr:   parts[2],
		SrcPort:   uint32(int64FromString(parts[3])),
		DstPort:   uint32(int64FromString(parts[4])),
		Proto:     azureProtocol(parts[5]),
		Direction: parts[6],
		Decision:  parts[7],
	}
	if len(parts) > 8 {
		out.State = parts[8]
	}
	if len(parts) > 9 {
		out.Packets += int64FromString(parts[9])
	}
	if len(parts) > 10 {
		out.Bytes += int64FromString(parts[10])
	}
	if len(parts) > 11 {
		out.Packets += int64FromString(parts[11])
	}
	if len(parts) > 12 {
		out.Bytes += int64FromString(parts[12])
	}
	return out, true
}

func azureProtocol(s string) uint32 {
	switch strings.ToUpper(s) {
	case "T", "TCP":
		return 6
	case "U", "UDP":
		return 17
	default:
		return uint32(int64FromString(s))
	}
}

func objectAt(m map[string]any, key string) map[string]any {
	val, ok := m[key]
	if !ok {
		return nil
	}
	obj, _ := val.(map[string]any)
	return obj
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

func copyAlias(dst map[string]any, src map[string]any, dstKey string, srcKeys ...string) {
	for _, key := range srcKeys {
		if val, ok := src[key]; ok {
			dst[dstKey] = val
			return
		}
	}
}

func setStringAlias(dst map[string]any, src map[string]any, dstKey string, srcKeys ...string) {
	if src == nil {
		return
	}
	if val := stringAlias(src, srcKeys...); val != "" {
		dst[dstKey] = val
	}
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

func setTimeAlias(dst map[string]any, src map[string]any, dstKey string, srcKeys ...string) {
	if src == nil {
		return
	}
	for _, key := range srcKeys {
		if val, ok := src[key]; ok {
			if ts, ok := unixFromAny(val); ok {
				dst[dstKey] = ts
				return
			}
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

func unixFromAny(val any) (int64, bool) {
	switch v := val.(type) {
	case float64:
		return int64(v) * 1000, true
	case int64:
		return v * 1000, true
	case int:
		return int64(v) * 1000, true
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UnixMilli(), true
		}
		n := int64FromString(v)
		if n != 0 {
			return n * 1000, true
		}
	}
	return 0, false
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
