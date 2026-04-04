package processor

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type Processor interface {
	Process(evt *event.Event) ([]*event.Event, error)
}

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

func NewBuiltin(cfg config.ProcessorConfig) *Builtin {
	return &Builtin{cfg: cfg.Builtin}
}

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

	if p.cfg.DropMessage {
		evt.Message = nil
	}

	return []*event.Event{evt}, nil
}
