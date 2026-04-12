package encode

import (
	"encoding/base64"
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

// New builds the configured encoder. Each encoder worker gets its own instance.
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

type JSONEncoder struct {
	flavor string
}

// NewJSONEncoder creates the stateless JSON event encoder.
func NewJSONEncoder(cfg config.EncoderConfig) *JSONEncoder {
	return &JSONEncoder{flavor: cfg.JSON.Flavor}
}

// Encode serializes one event as a JSON payload in the configured output flavor.
func (e JSONEncoder) Encode(evt *event.Event) ([][]byte, error) {
	payload := e.formatEvent(evt)
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return [][]byte{data}, nil
}

// Flush is a no-op for JSON because it does not keep internal batching state.
func (JSONEncoder) Flush() ([][]byte, error) {
	return nil, nil
}

func (e JSONEncoder) formatEvent(evt *event.Event) any {
	switch e.flavor {
	case "", "canonical":
		return evt
	case "vendor":
		return map[string]any{
			"src_addr":         stringFieldOrZero(evt.Fields, "src_addr"),
			"dst_addr":         stringFieldOrZero(evt.Fields, "dst_addr"),
			"src_port":         uint32Field(evt.Fields, "src_port"),
			"dst_port":         uint32Field(evt.Fields, "dst_port"),
			"proto":            uint32Field(evt.Fields, "proto"),
			"packets":          int64Field(evt.Fields, "packets"),
			"bytes":            int64Field(evt.Fields, "bytes"),
			"start_time_unix":  int64Field(evt.Fields, "start_time_unix"),
			"end_time_unix":    int64Field(evt.Fields, "end_time_unix"),
			"flow_direction":   stringFieldOrZero(evt.Fields, "flow_direction"),
			"traffic_decision": stringFieldOrZero(evt.Fields, "traffic_decision"),
			"action":           stringFieldOrZero(evt.Fields, "action"),
			"log_status":       stringFieldOrZero(evt.Fields, "log_status"),
			"reporter":         stringFieldOrZero(evt.Fields, "reporter"),
			"disposition":      stringFieldOrZero(evt.Fields, "disposition"),
		}
	case "goflow2v2":
		out := map[string]any{
			"sampler_address":    encodeIPBytes(stringFieldOrZero(evt.Fields, "agent_ip")),
			"src_addr":           encodeIPBytes(stringFieldOrZero(evt.Fields, "src_addr")),
			"dst_addr":           encodeIPBytes(stringFieldOrZero(evt.Fields, "dst_addr")),
			"src_port":           uint32Field(evt.Fields, "src_port"),
			"dst_port":           uint32Field(evt.Fields, "dst_port"),
			"proto":              uint32Field(evt.Fields, "proto"),
			"bytes":              int64Field(evt.Fields, "bytes"),
			"packets":            int64Field(evt.Fields, "packets"),
			"time_flow_start_ns": int64Field(evt.Fields, "start_time_unix") * int64(time.Millisecond),
			"time_flow_end_ns":   int64Field(evt.Fields, "end_time_unix") * int64(time.Millisecond),
			"sampling_rate":      uint32Field(evt.Fields, "sampling_rate"),
			"in_if":              uint32Field(evt.Fields, "input_if"),
			"out_if":             uint32Field(evt.Fields, "output_if"),
			"type":               flowTypeField(evt.Fields),
		}
		return out
	default:
		return evt
	}
}

type SFlowEncoder struct {
	seq              atomic.Uint32
	started          time.Time
	maxDatagramBytes int
	batch            config.BatchConfig
	cfg              config.SFlowConfig
	events           []*event.Event
}

func NewSFlowEncoder(cfg config.EncoderConfig) *SFlowEncoder {
	return &SFlowEncoder{
		started:          time.Now(),
		maxDatagramBytes: cfg.MaxDatagramBytes,
		batch:            cfg.Batch,
		cfg:              cfg.SFlow,
	}
}

// Encode appends an event to the encoder-local batch or encodes it immediately.
func (e *SFlowEncoder) Encode(evt *event.Event) ([][]byte, error) {
	if !e.batch.Enabled {
		packet, err := e.buildPacket([]*event.Event{evt})
		if err != nil {
			return nil, err
		}
		return e.encodePacket(packet)
	}

	if len(e.events) > 0 && !e.compatibleTopLevel(e.events[0], evt) {
		payloads, err := e.Flush()
		if err != nil {
			return nil, err
		}
		e.events = append(e.events, evt)
		if e.shouldFlush() {
			flushed, err := e.Flush()
			if err != nil {
				return nil, err
			}
			return append(payloads, flushed...), nil
		}
		return payloads, nil
	}

	e.events = append(e.events, evt)
	if e.shouldFlush() {
		return e.Flush()
	}
	return nil, nil
}

// Flush emits all buffered events, splitting them into multiple sFlow datagrams if needed.
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

// shouldFlush checks the configured batch thresholds before the timer fires.
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

// estimatedBatchBytes provides a cheap threshold check before building an actual packet.
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

// encodePacket turns one populated sFlow packet into the UDP payload sent by the sink.
func (e *SFlowEncoder) encodePacket(packet *sflow.Packet) ([][]byte, error) {
	data, err := sflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode sflow packet: %w", err)
	}
	return [][]byte{data}, nil
}

// buildPacket requires all input events to fit in a single datagram.
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

// buildPacketWithLimit packs as many events as possible into one sFlow datagram.
func (e *SFlowEncoder) buildPacketWithLimit(events []*event.Event) (*sflow.Packet, int, error) {
	if len(events) == 0 {
		return nil, 0, fmt.Errorf("empty sflow packet batch")
	}

	first := events[0]
	top, err := e.packetTopLevel(first)
	if err != nil {
		return nil, 0, err
	}
	addr, err := netip.ParseAddr(top.AgentIP)
	if err != nil {
		return nil, 0, fmt.Errorf("parse agent_ip %q: %w", top.AgentIP, err)
	}

	packetSeq := top.SequenceNumber
	if packetSeq == 0 {
		packetSeq = e.seq.Add(1)
	}
	uptime := top.Uptime
	if uptime == 0 {
		uptime = uint32(time.Since(e.started).Milliseconds())
	}
	packet := &sflow.Packet{
		Version:        5,
		AgentIP:        utils.IPAddress(addr.AsSlice()),
		SubAgentId:     top.SubAgentID,
		SequenceNumber: packetSeq,
		Uptime:         uptime,
		Samples:        make([]interface{}, 0, len(events)),
	}

	accepted := 0
	for _, evt := range events {
		if accepted > 0 && !e.compatibleTopLevel(first, evt) {
			break
		}
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

// buildFlowSample maps the canonical event fields into one sFlow raw-header flow sample.
func (e *SFlowEncoder) buildFlowSample(evt *event.Event) (sflow.FlowSample, error) {
	fields := evt.Fields
	if fields == nil {
		return sflow.FlowSample{}, fmt.Errorf("event fields are empty")
	}
	sf := evt.SFlow

	return sflow.FlowSample{
		Header: sflow.SampleHeader{
			Format:               sflow.SAMPLE_FORMAT_FLOW,
			SampleSequenceNumber: e.sampleSequence(evt),
			SourceIdType:         0,
			SourceIdValue:        sflowSourceID(sf, fields),
		},
		SamplingRate: sflowSamplingRate(sf, fields),
		SamplePool:   sflowSamplePool(sf, fields),
		Drops:        sflowDrops(sf, fields),
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

type sflowPacketTopLevel struct {
	AgentIP        string
	SubAgentID     uint32
	SequenceNumber uint32
	Uptime         uint32
}

func (e *SFlowEncoder) packetTopLevel(evt *event.Event) (sflowPacketTopLevel, error) {
	top := sflowPacketTopLevel{
		AgentIP:        e.sflowAgentIP(evt),
		SubAgentID:     sflowSubAgentID(evt.SFlow, evt.Fields),
		SequenceNumber: sflowSequenceNumber(evt.SFlow),
		Uptime:         sflowUptime(evt.SFlow),
	}
	if top.AgentIP == "" {
		return sflowPacketTopLevel{}, fmt.Errorf("missing field \"agent_ip\"")
	}
	return top, nil
}

func (e *SFlowEncoder) sflowAgentIP(evt *event.Event) string {
	if e.cfg.AgentIP != "" {
		return e.cfg.AgentIP
	}
	if evt.SFlow != nil && evt.SFlow.AgentIP != "" {
		return evt.SFlow.AgentIP
	}
	return stringFieldOrZero(evt.Fields, "agent_ip")
}

func (e *SFlowEncoder) compatibleTopLevel(left, right *event.Event) bool {
	leftTop, err := e.packetTopLevel(left)
	if err != nil {
		return false
	}
	rightTop, err := e.packetTopLevel(right)
	if err != nil {
		return false
	}
	if !batchOverEnabled(e.cfg.BatchOver.AgentIP) && leftTop.AgentIP != rightTop.AgentIP {
		return false
	}
	if !batchOverEnabled(e.cfg.BatchOver.SubAgentID) && leftTop.SubAgentID != rightTop.SubAgentID {
		return false
	}
	if !batchOverEnabled(e.cfg.BatchOver.SequenceNumber) && leftTop.SequenceNumber != rightTop.SequenceNumber {
		return false
	}
	if !batchOverEnabled(e.cfg.BatchOver.Uptime) && leftTop.Uptime != rightTop.Uptime {
		return false
	}
	return true
}

func (e *SFlowEncoder) sampleSequence(evt *event.Event) uint32 {
	if evt.SFlow != nil && evt.SFlow.SequenceNumber != 0 {
		return evt.SFlow.SequenceNumber
	}
	return e.seq.Add(1)
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

func stringFieldOrZero(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	val, ok := fields[key]
	if !ok {
		return ""
	}
	s, _ := val.(string)
	return s
}

func uint32Field(fields map[string]any, key string) uint32 {
	if fields == nil {
		return 0
	}
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

func int64Field(fields map[string]any, key string) int64 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case uint64:
		return int64(v)
	case int:
		return int64(v)
	case uint32:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

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

func sflowSubAgentID(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.SubAgentID != 0 {
		return sf.SubAgentID
	}
	return uint32Field(fields, "sub_agent_id")
}

func sflowSequenceNumber(sf *event.SFlowMetadata) uint32 {
	if sf == nil {
		return 0
	}
	return sf.SequenceNumber
}

func sflowUptime(sf *event.SFlowMetadata) uint32 {
	if sf == nil {
		return 0
	}
	return sf.Uptime
}

func sflowSourceID(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.SourceID != 0 {
		return sf.SourceID
	}
	return uint32Field(fields, "source_id")
}

func sflowSamplingRate(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.SamplingRate != 0 {
		return sf.SamplingRate
	}
	return uint32Field(fields, "sampling_rate")
}

func sflowSamplePool(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.SamplePool != 0 {
		return sf.SamplePool
	}
	return uint32Field(fields, "sample_pool")
}

func sflowDrops(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.Drops != 0 {
		return sf.Drops
	}
	return uint32Field(fields, "drops")
}

func batchOverEnabled(v *bool) bool {
	return v == nil || *v
}

func encodeIPBytes(ip string) string {
	if ip == "" {
		return ""
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(addr.AsSlice())
}

func flowTypeField(fields map[string]any) any {
	val := stringFieldOrZero(fields, "flow_type")
	switch val {
	case "sflow":
		return 1
	case "netflowv5":
		return 2
	case "netflowv9":
		return 3
	case "ipfix":
		return 4
	case "":
		return 0
	default:
		return val
	}
}
