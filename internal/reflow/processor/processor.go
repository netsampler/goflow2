package processor

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"

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
	switch evt.Source.MessageType {
	case "json_raw_packet_header":
		return p.processJSONRawPacketHeader(evt)
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

	if evt.Fields == nil {
		evt.Fields = make(map[string]any, 12)
	}
	evt.Fields["agent_ip"] = in.AgentIP
	evt.Fields["sub_agent_id"] = in.SubAgentID
	evt.Fields["source_id"] = in.SourceID
	evt.Fields["sampling_rate"] = in.SamplingRate
	evt.Fields["sample_pool"] = in.SamplePool
	evt.Fields["drops"] = in.Drops
	evt.Fields["input_if"] = in.InputIf
	evt.Fields["output_if"] = in.OutputIf
	evt.Fields["protocol"] = in.Protocol
	evt.Fields["frame_length"] = in.FrameLength
	evt.Fields["stripped"] = in.Stripped
	evt.Fields["original_length"] = in.OriginalLength
	evt.Fields["header_data"] = headerData
	evt.Fields["message_type"] = evt.Source.MessageType
	evt.Fields["bytes"] = int64(in.OriginalLength)
	evt.Fields["packets"] = int64(1)

	if tuple, err := parsePacketTuple(headerData); err == nil {
		evt.Fields["src_addr"] = tuple.SrcAddr.String()
		evt.Fields["dst_addr"] = tuple.DstAddr.String()
		evt.Fields["proto"] = tuple.Proto
		evt.Fields["src_port"] = tuple.SrcPort
		evt.Fields["dst_port"] = tuple.DstPort
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
