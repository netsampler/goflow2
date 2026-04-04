package encode

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type Encoder interface {
	Encode(evt *event.Event) ([][]byte, error)
}

func New(cfg config.EncoderConfig) (Encoder, error) {
	switch cfg.Type {
	case "", "json":
		return JSONEncoder{}, nil
	case "sflow":
		return NewSFlowEncoder(), nil
	default:
		return nil, fmt.Errorf("unsupported encoder.type %q", cfg.Type)
	}
}

type JSONEncoder struct{}

func (JSONEncoder) Encode(evt *event.Event) ([][]byte, error) {
	data, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return [][]byte{data}, nil
}

type SFlowEncoder struct {
	seq     atomic.Uint32
	started time.Time
}

func NewSFlowEncoder() *SFlowEncoder {
	return &SFlowEncoder{started: time.Now()}
}

func (e *SFlowEncoder) Encode(evt *event.Event) ([][]byte, error) {
	packet, err := e.buildPacket(evt)
	if err != nil {
		return nil, err
	}
	data, err := sflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode sflow packet: %w", err)
	}
	return [][]byte{data}, nil
}

func (e *SFlowEncoder) buildPacket(evt *event.Event) (*sflow.Packet, error) {
	fields := evt.Fields
	if fields == nil {
		return nil, fmt.Errorf("event fields are empty")
	}

	agentIPStr, err := stringField(fields, "agent_ip")
	if err != nil {
		return nil, err
	}
	addr, err := netip.ParseAddr(agentIPStr)
	if err != nil {
		return nil, fmt.Errorf("parse agent_ip %q: %w", agentIPStr, err)
	}

	seq := e.seq.Add(1)
	packet := &sflow.Packet{
		Version:        5,
		AgentIP:        utils.IPAddress(addr.AsSlice()),
		SubAgentId:     uint32Field(fields, "sub_agent_id"),
		SequenceNumber: seq,
		Uptime:         uint32(time.Since(e.started).Milliseconds()),
		Samples: []interface{}{
			sflow.FlowSample{
				Header: sflow.SampleHeader{
					Format:               sflow.SAMPLE_FORMAT_FLOW,
					SampleSequenceNumber: seq,
					SourceIdType:         0,
					SourceIdValue:        uint32Field(fields, "source_id"),
				},
				SamplingRate: uint32Field(fields, "sampling_rate"),
				SamplePool:   uint32Field(fields, "sample_pool"),
				Drops:        uint32Field(fields, "drops"),
				Input:        uint32Field(fields, "input_if"),
				Output:       uint32Field(fields, "output_if"),
				Records: []sflow.FlowRecord{
					{
						Data: sflow.SampledHeader{
							Protocol:       uint32Field(fields, "protocol"),
							FrameLength:    uint32Field(fields, "frame_length"),
							Stripped:       uint32Field(fields, "stripped"),
							OriginalLength: uint32Field(fields, "original_length"),
							HeaderData:     bytesField(fields, "header_data"),
						},
					},
				},
			},
		},
	}
	return packet, nil
}

func stringField(fields map[string]any, key string) (string, error) {
	val, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("missing field %q", key)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("field %q is not a string", key)
	}
	return s, nil
}

func uint32Field(fields map[string]any, key string) uint32 {
	val, ok := fields[key]
	if !ok {
		return 0
	}
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
	default:
		return 0
	}
}

func bytesField(fields map[string]any, key string) []byte {
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
