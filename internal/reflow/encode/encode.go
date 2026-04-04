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
	Flush() ([][]byte, error)
}

func New(cfg config.EncoderConfig) (Encoder, error) {
	switch cfg.Type {
	case "", "json":
		return NewJSONEncoder(cfg), nil
	case "sflow":
		return NewSFlowEncoder(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported encoder.type %q", cfg.Type)
	}
}

type JSONEncoder struct{}

func NewJSONEncoder(_ config.EncoderConfig) *JSONEncoder {
	return &JSONEncoder{}
}

func (JSONEncoder) Encode(evt *event.Event) ([][]byte, error) {
	data, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return [][]byte{data}, nil
}

func (JSONEncoder) Flush() ([][]byte, error) {
	return nil, nil
}

type SFlowEncoder struct {
	seq              atomic.Uint32
	started          time.Time
	maxDatagramBytes int
	batch            config.BatchConfig
	events           []*event.Event
}

func NewSFlowEncoder(cfg config.EncoderConfig) *SFlowEncoder {
	return &SFlowEncoder{
		started:          time.Now(),
		maxDatagramBytes: cfg.MaxDatagramBytes,
		batch:            cfg.Batch,
	}
}

func (e *SFlowEncoder) Encode(evt *event.Event) ([][]byte, error) {
	if !e.batch.Enabled {
		packet, err := e.buildPacket([]*event.Event{evt})
		if err != nil {
			return nil, err
		}
		return e.encodePacket(packet)
	}

	e.events = append(e.events, evt)
	if e.shouldFlush() {
		return e.Flush()
	}
	return nil, nil
}

func (e *SFlowEncoder) Flush() ([][]byte, error) {
	if len(e.events) == 0 {
		return nil, nil
	}

	var payloads [][]byte
	pending := e.events
	e.events = nil

	for len(pending) > 0 {
		packet, accepted, err := e.buildPacketWithLimit(pending)
		if err != nil {
			return nil, err
		}
		encoded, err := e.encodePacket(packet)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, encoded...)
		pending = pending[accepted:]
	}

	return payloads, nil
}

func (e *SFlowEncoder) shouldFlush() bool {
	if len(e.events) == 0 {
		return false
	}
	if e.batch.MaxRecords > 0 && len(e.events) >= e.batch.MaxRecords {
		return true
	}
	if e.batch.MaxBytes > 0 && e.estimatedBatchBytes() >= e.batch.MaxBytes {
		return true
	}
	return false
}

func (e *SFlowEncoder) estimatedBatchBytes() int {
	total := 0
	for _, evt := range e.events {
		total += estimatedEventSize(evt)
	}
	return total
}

func estimatedEventSize(evt *event.Event) int {
	total := len(evt.Message) + 128
	for key, val := range evt.Fields {
		total += len(key)
		switch v := val.(type) {
		case string:
			total += len(v)
		case []byte:
			total += len(v)
		default:
			total += 16
		}
	}
	return total
}

func (e *SFlowEncoder) encodePacket(packet *sflow.Packet) ([][]byte, error) {
	data, err := sflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode sflow packet: %w", err)
	}
	return [][]byte{data}, nil
}

func (e *SFlowEncoder) buildPacket(events []*event.Event) (*sflow.Packet, error) {
	packet, accepted, err := e.buildPacketWithLimit(events)
	if err != nil {
		return nil, err
	}
	if accepted != len(events) {
		return nil, fmt.Errorf("sflow packet exceeds max_datagram_bytes=%d", e.maxDatagramBytes)
	}
	return packet, nil
}

func (e *SFlowEncoder) buildPacketWithLimit(events []*event.Event) (*sflow.Packet, int, error) {
	if len(events) == 0 {
		return nil, 0, fmt.Errorf("empty sflow packet batch")
	}

	first := events[0]
	fields := first.Fields
	if fields == nil {
		return nil, 0, fmt.Errorf("event fields are empty")
	}

	agentIPStr, err := stringField(fields, "agent_ip")
	if err != nil {
		return nil, 0, err
	}
	addr, err := netip.ParseAddr(agentIPStr)
	if err != nil {
		return nil, 0, fmt.Errorf("parse agent_ip %q: %w", agentIPStr, err)
	}

	packetSeq := e.seq.Add(1)
	packet := &sflow.Packet{
		Version:        5,
		AgentIP:        utils.IPAddress(addr.AsSlice()),
		SubAgentId:     uint32Field(fields, "sub_agent_id"),
		SequenceNumber: packetSeq,
		Uptime:         uint32(time.Since(e.started).Milliseconds()),
		Samples:        make([]interface{}, 0, len(events)),
	}

	accepted := 0
	for _, evt := range events {
		sample, err := e.buildFlowSample(evt)
		if err != nil {
			return nil, accepted, err
		}
		packet.Samples = append(packet.Samples, sample)
		if e.maxDatagramBytes > 0 {
			data, err := sflow.EncodeMessage(packet)
			if err != nil {
				return nil, accepted, fmt.Errorf("encode sflow packet: %w", err)
			}
			if len(data) > e.maxDatagramBytes {
				packet.Samples = packet.Samples[:len(packet.Samples)-1]
				break
			}
		}
		accepted++
	}

	if accepted == 0 {
		return nil, 0, fmt.Errorf("sflow sample exceeds max_datagram_bytes=%d", e.maxDatagramBytes)
	}

	packet.SamplesCount = uint32(len(packet.Samples))
	return packet, accepted, nil
}

func (e *SFlowEncoder) buildFlowSample(evt *event.Event) (sflow.FlowSample, error) {
	fields := evt.Fields
	if fields == nil {
		return sflow.FlowSample{}, fmt.Errorf("event fields are empty")
	}

	return sflow.FlowSample{
		Header: sflow.SampleHeader{
			Format:               sflow.SAMPLE_FORMAT_FLOW,
			SampleSequenceNumber: e.seq.Add(1),
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
	}, nil
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
