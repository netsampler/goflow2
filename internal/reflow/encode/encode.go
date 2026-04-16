package encode

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sort"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	flowpb "github.com/netsampler/goflow2/v3/pb"
	"google.golang.org/protobuf/encoding/protowire"
)

type Encoder interface {
	Encode(evt *event.Event) ([][]byte, error)
	Flush() ([][]byte, error)
}

var ErrSFlowSampleTooLarge = errors.New("sflow sample exceeds max_datagram_bytes")

type sflowSampleTooLargeError struct {
	MaxDatagramBytes int
	CurrentSize      int
}

// Error reports both the configured datagram limit and the offending packet size.
func (e *sflowSampleTooLargeError) Error() string {
	return fmt.Sprintf("%s: current_size=%d max_datagram_bytes=%d", ErrSFlowSampleTooLarge, e.CurrentSize, e.MaxDatagramBytes)
}

// Unwrap lets callers treat the concrete size error as ErrSFlowSampleTooLarge.
func (e *sflowSampleTooLargeError) Unwrap() error {
	return ErrSFlowSampleTooLarge
}

// New builds the configured encoder. Each encoder worker gets its own instance.
func New(cfg config.EncoderConfig) (Encoder, error) {
	switch cfg.Type {
	case "", "json":
		return NewJSONEncoder(cfg), nil
	case "protobuf":
		return NewProtobufEncoder(cfg)
	case "sflow":
		return NewSFlowEncoder(cfg), nil
	case "ipfix":
		return NewIPFIXEncoder(cfg), nil
	case "netflowv9":
		return NewNFv9Encoder(cfg), nil
	case "netflowv5":
		return NewNFv5Encoder(cfg), nil
	default:
		return nil, fmt.Errorf("unsupported encoder.type %q", cfg.Type)
	}
}

type JSONEncoder struct {
	flavor     string
	dropFields map[string]struct{}
}

type protobufFieldSpec struct {
	name   string
	append func([]byte, *event.Event) ([]byte, bool, error)
}

type ProtobufEncoder struct {
	flavor         string
	lengthPrefixed bool
	fields         []protobufFieldSpec
}

// NewJSONEncoder creates the stateless JSON event encoder.
func NewJSONEncoder(cfg config.EncoderConfig) *JSONEncoder {
	dropFields := make(map[string]struct{}, len(cfg.JSON.DropFields))
	for _, field := range cfg.JSON.DropFields {
		dropFields[field] = struct{}{}
	}
	return &JSONEncoder{
		flavor:     cfg.JSON.Flavor,
		dropFields: dropFields,
	}
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

// formatEvent selects the output view used for one JSON-encoded event.
func (e JSONEncoder) formatEvent(evt *event.Event) any {
	switch e.flavor {
	case "", "canonical":
		return e.filterEvent(evt)
	case "vendor":
		return e.filterMap(map[string]any{
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
		})
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
		return e.filterMap(out)
	default:
		return e.filterEvent(evt)
	}
}

// filterEvent preserves the original event when no fields were dropped to avoid
// allocating a shallow copy for the common case.
func (e JSONEncoder) filterEvent(evt *event.Event) any {
	if len(e.dropFields) == 0 || len(evt.Fields) == 0 {
		return evt
	}

	filteredFields := e.filterMap(evt.Fields)
	if len(filteredFields) == len(evt.Fields) {
		return evt
	}

	filtered := *evt
	filtered.Fields = filteredFields
	return &filtered
}

// filterMap applies the configured drop-fields policy to a map payload.
func (e JSONEncoder) filterMap(fields map[string]any) map[string]any {
	if len(fields) == 0 || len(e.dropFields) == 0 {
		return fields
	}

	filtered := make(map[string]any, len(fields))
	for key, value := range fields {
		if _, drop := e.dropFields[key]; drop {
			continue
		}
		filtered[key] = value
	}
	return filtered
}

func NewProtobufEncoder(cfg config.EncoderConfig) (*ProtobufEncoder, error) {
	fields, err := compileProtobufFieldPlan(cfg.Protobuf.Flavor)
	if err != nil {
		return nil, err
	}
	return &ProtobufEncoder{
		flavor:         cfg.Protobuf.Flavor,
		lengthPrefixed: cfg.Protobuf.LengthPrefixed,
		fields:         fields,
	}, nil
}

func (e *ProtobufEncoder) Encode(evt *event.Event) ([][]byte, error) {
	buf := make([]byte, 0, 256)
	for _, field := range e.fields {
		var ok bool
		var err error
		buf, ok, err = field.append(buf, evt)
		if err != nil {
			return nil, fmt.Errorf("encode protobuf field %s: %w", field.name, err)
		}
		if !ok {
			continue
		}
	}

	if e.lengthPrefixed {
		framed := protowire.AppendVarint(make([]byte, 0, len(buf)+10), uint64(len(buf)))
		framed = append(framed, buf...)
		return [][]byte{framed}, nil
	}
	return [][]byte{buf}, nil
}

func (e *ProtobufEncoder) Flush() ([][]byte, error) {
	return nil, nil
}

func compileProtobufFieldPlan(flavor string) ([]protobufFieldSpec, error) {
	switch flavor {
	case "", "canonical", "goflow2v2":
		return []protobufFieldSpec{
			protobufEnumField("type", 1, protobufFlowTypeValue),
			protobufUint64Field("time_received_ns", 110, func(evt *event.Event) uint64 {
				if evt == nil || evt.ReceivedAt.IsZero() {
					return 0
				}
				return uint64(evt.ReceivedAt.UnixNano())
			}),
			protobufUint32Field("sequence_num", 4, func(evt *event.Event) uint32 {
				if evt != nil && evt.SFlow != nil && evt.SFlow.SequenceNumber != 0 {
					return evt.SFlow.SequenceNumber
				}
				return uint32Field(evt.Fields, "sequence_num")
			}),
			protobufUint64Field("sampling_rate", 3, func(evt *event.Event) uint64 {
				if evt != nil && evt.SFlow != nil && evt.SFlow.SamplingRate != 0 {
					return uint64(evt.SFlow.SamplingRate)
				}
				return uint64(uint32Field(evt.Fields, "sampling_rate"))
			}),
			protobufIPField("sampler_address", 11, func(evt *event.Event) string {
				if evt != nil && evt.SFlow != nil && evt.SFlow.AgentIP != "" {
					return evt.SFlow.AgentIP
				}
				return stringFieldOrZero(evt.Fields, "agent_ip")
			}),
			protobufUint64Field("time_flow_start_ns", 111, func(evt *event.Event) uint64 {
				startMS := int64Field(evt.Fields, "start_time_unix")
				if startMS == 0 {
					return 0
				}
				return uint64(startMS) * uint64(time.Millisecond)
			}),
			protobufUint64Field("time_flow_end_ns", 112, func(evt *event.Event) uint64 {
				endMS := int64Field(evt.Fields, "end_time_unix")
				if endMS == 0 {
					return 0
				}
				return uint64(endMS) * uint64(time.Millisecond)
			}),
			protobufUint64Field("bytes", 9, func(evt *event.Event) uint64 {
				return uint64Field(evt.Fields, "bytes")
			}),
			protobufUint64Field("packets", 10, func(evt *event.Event) uint64 {
				return uint64Field(evt.Fields, "packets")
			}),
			protobufIPField("src_addr", 6, func(evt *event.Event) string {
				return stringFieldOrZero(evt.Fields, "src_addr")
			}),
			protobufIPField("dst_addr", 7, func(evt *event.Event) string {
				return stringFieldOrZero(evt.Fields, "dst_addr")
			}),
			protobufUint32Field("etype", 30, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "etype")
			}),
			protobufUint32Field("proto", 20, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "proto")
			}),
			protobufUint32Field("src_port", 21, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "src_port")
			}),
			protobufUint32Field("dst_port", 22, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "dst_port")
			}),
			protobufUint32Field("in_if", 18, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "input_if")
			}),
			protobufUint32Field("out_if", 19, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "output_if")
			}),
			protobufUint32Field("observation_domain_id", 70, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "observation_domain_id")
			}),
			protobufUint32Field("observation_point_id", 71, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "observation_point_id")
			}),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported protobuf flavor %q", flavor)
	}
}

func protobufUint32Field(name string, num protowire.Number, get func(*event.Event) uint32) protobufFieldSpec {
	return protobufFieldSpec{
		name: name,
		append: func(dst []byte, evt *event.Event) ([]byte, bool, error) {
			val := get(evt)
			if val == 0 {
				return dst, false, nil
			}
			dst = protowire.AppendTag(dst, num, protowire.VarintType)
			dst = protowire.AppendVarint(dst, uint64(val))
			return dst, true, nil
		},
	}
}

func protobufUint64Field(name string, num protowire.Number, get func(*event.Event) uint64) protobufFieldSpec {
	return protobufFieldSpec{
		name: name,
		append: func(dst []byte, evt *event.Event) ([]byte, bool, error) {
			val := get(evt)
			if val == 0 {
				return dst, false, nil
			}
			dst = protowire.AppendTag(dst, num, protowire.VarintType)
			dst = protowire.AppendVarint(dst, val)
			return dst, true, nil
		},
	}
}

func protobufIPField(name string, num protowire.Number, get func(*event.Event) string) protobufFieldSpec {
	return protobufFieldSpec{
		name: name,
		append: func(dst []byte, evt *event.Event) ([]byte, bool, error) {
			ip := get(evt)
			if ip == "" {
				return dst, false, nil
			}
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				return dst, false, nil
			}
			dst = protowire.AppendTag(dst, num, protowire.BytesType)
			dst = protowire.AppendBytes(dst, addr.AsSlice())
			return dst, true, nil
		},
	}
}

func protobufEnumField(name string, num protowire.Number, get func(*event.Event) uint64) protobufFieldSpec {
	return protobufFieldSpec{
		name: name,
		append: func(dst []byte, evt *event.Event) ([]byte, bool, error) {
			val := get(evt)
			if val == 0 {
				return dst, false, nil
			}
			dst = protowire.AppendTag(dst, num, protowire.VarintType)
			dst = protowire.AppendVarint(dst, val)
			return dst, true, nil
		},
	}
}

func protobufFlowTypeValue(evt *event.Event) uint64 {
	if evt == nil {
		return 0
	}
	if evt.Fields != nil {
		switch stringFieldOrZero(evt.Fields, "counter_type") {
		case "sflow":
			return uint64(flowpb.FlowMessage_SFLOW_5)
		}
		switch val := flowTypeField(evt.Fields).(type) {
		case int:
			return uint64(val)
		case uint32:
			return uint64(val)
		case uint64:
			return val
		}
	}
	if evt.SFlow != nil {
		return uint64(flowpb.FlowMessage_SFLOW_5)
	}
	return 0
}

type SFlowEncoder struct {
	packetSeq        atomic.Uint32
	sampleSeq        atomic.Uint32
	started          time.Time
	maxDatagramBytes int
	allowTruncate    bool
	batch            config.BatchConfig
	cfg              config.SFlowConfig
	events           []*event.Event
}

type IPFIXEncoder struct {
	seq             atomic.Uint32
	cfg             config.EncoderConfig
	dataSchemas     map[string]templatedSchemaState
	sourceOptions   map[string]sourceOptionsState
	lastTemplateRun time.Time
	lastOptionsRun  time.Time
}

type NFv9Encoder struct {
	seq             atomic.Uint32
	cfg             config.EncoderConfig
	dataSchemas     map[string]templatedSchemaState
	sourceOptions   map[string]sourceOptionsState
	lastTemplateRun time.Time
	lastOptionsRun  time.Time
}

type NFv5Encoder struct {
	seq atomic.Uint32
}

type templatedSchemaState struct {
	stream         string
	fieldNames     []string
	baseTemplateID uint16
	ipv4Template   netflow.TemplateRecord
	ipv6Template   netflow.TemplateRecord
	hasIPv6Variant bool
}

type sourceOptionsState struct {
	stream              string
	agentIP             string
	sourceID            uint32
	observationDomainID uint32
	samplingRate        uint32
	samplePool          uint32
	drops               uint32
	inputIf             uint32
	outputIf            uint32
	templateID          uint16
}

func NewIPFIXEncoder(cfg config.EncoderConfig) *IPFIXEncoder {
	return &IPFIXEncoder{
		cfg:           cfg,
		dataSchemas:   make(map[string]templatedSchemaState),
		sourceOptions: make(map[string]sourceOptionsState),
	}
}

func (e *IPFIXEncoder) Encode(evt *event.Event) ([][]byte, error) {
	if evt != nil && evt.Kind == "control" {
		return e.handleControl(evt)
	}
	packet, err := e.buildPacket(evt)
	if err != nil {
		return nil, err
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode ipfix packet: %w", err)
	}
	return [][]byte{data}, nil
}

func (e *IPFIXEncoder) Flush() ([][]byte, error) {
	return e.flushControlPackets(time.Now().UTC())
}

func NewNFv9Encoder(cfg config.EncoderConfig) *NFv9Encoder {
	return &NFv9Encoder{
		cfg:           cfg,
		dataSchemas:   make(map[string]templatedSchemaState),
		sourceOptions: make(map[string]sourceOptionsState),
	}
}

func (e *NFv9Encoder) Encode(evt *event.Event) ([][]byte, error) {
	if evt != nil && evt.Kind == "control" {
		return e.handleControl(evt)
	}
	packet, err := e.buildPacket(evt)
	if err != nil {
		return nil, err
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode netflow v9 packet: %w", err)
	}
	return [][]byte{data}, nil
}

func (e *NFv9Encoder) Flush() ([][]byte, error) {
	return e.flushControlPackets(time.Now().UTC())
}

func NewNFv5Encoder(cfg config.EncoderConfig) *NFv5Encoder {
	return &NFv5Encoder{}
}

// Encode turns one canonical event into one NetFlow v5 datagram.
func (e *NFv5Encoder) Encode(evt *event.Event) ([][]byte, error) {
	packet, err := e.buildPacket(evt)
	if err != nil {
		return nil, err
	}
	data, err := netflowlegacy.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode netflow v5 packet: %w", err)
	}
	return [][]byte{data}, nil
}

// Flush is a no-op because the NetFlow v5 encoder is stateless.
func (e *NFv5Encoder) Flush() ([][]byte, error) {
	return nil, nil
}

// NewSFlowEncoder builds one encoder instance per runtime worker. Each instance
// keeps its own batch buffer and sequence counters.
func NewSFlowEncoder(cfg config.EncoderConfig) *SFlowEncoder {
	return &SFlowEncoder{
		started:          time.Now(),
		maxDatagramBytes: cfg.MaxDatagramBytes,
		allowTruncate:    cfg.AllowTruncate,
		batch:            cfg.Batch,
		cfg:              cfg.SFlow,
	}
}

// Encode appends an event to the encoder-local batch or encodes it immediately.
func (e *SFlowEncoder) Encode(evt *event.Event) ([][]byte, error) {
	if !e.batch.Enabled {
		packet, err := e.buildPacket([]*event.Event{evt})
		if err != nil {
			if errors.Is(err, ErrSFlowSampleTooLarge) {
				logOversizedSample(err)
				return nil, nil
			}
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
			if errors.Is(err, ErrSFlowSampleTooLarge) {
				logOversizedSample(err)
				pending = pending[1:]
				continue
			}
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
		packetSeq = e.packetSeq.Add(1)
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
		sample, err := e.buildSample(evt)
		if err != nil {
			return nil, accepted, err
		}
		packet.Samples = append(packet.Samples, sample)
		lastSize := 0
		if e.maxDatagramBytes > 0 {
			data, err := sflow.EncodeMessage(packet)
			if err != nil {
				return nil, accepted, fmt.Errorf("encode sflow packet: %w", err)
			}
			lastSize = len(data)
			if len(data) > e.maxDatagramBytes {
				if e.allowTruncate {
					truncated, ok, err := e.truncateLastSampleToFit(packet)
					if err != nil {
						return nil, accepted, err
					}
					if ok {
						packet.Samples[len(packet.Samples)-1] = truncated
						accepted++
						continue
					}
				}
				packet.Samples = packet.Samples[:len(packet.Samples)-1]
				if accepted == 0 {
					return nil, 0, &sflowSampleTooLargeError{
						MaxDatagramBytes: e.maxDatagramBytes,
						CurrentSize:      lastSize,
					}
				}
				break
			}
		}
		accepted++
	}

	if accepted == 0 {
		return nil, 0, &sflowSampleTooLargeError{
			MaxDatagramBytes: e.maxDatagramBytes,
			CurrentSize:      0,
		}
	}

	packet.SamplesCount = uint32(len(packet.Samples))
	return packet, accepted, nil
}

// buildSample dispatches between flow-sample and counter-sample output.
func (e *SFlowEncoder) buildSample(evt *event.Event) (interface{}, error) {
	if isSFlowCounterEvent(evt) {
		return e.buildCounterSample(evt)
	}
	return e.buildFlowSample(evt)
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

func (e *SFlowEncoder) buildCounterSample(evt *event.Event) (sflow.CounterSample, error) {
	fields := evt.Fields
	if fields == nil {
		return sflow.CounterSample{}, fmt.Errorf("event fields are empty")
	}
	sf := evt.SFlow
	format, sourceIDType := e.counterSampleFormat(fields)

	return sflow.CounterSample{
		Header: sflow.SampleHeader{
			Format:               format,
			SampleSequenceNumber: e.sampleSequence(evt),
			SourceIdType:         sourceIDType,
			SourceIdValue:        sflowSourceID(sf, fields),
		},
		CounterRecordsCount: 1,
		Records: []sflow.CounterRecord{
			{
				Data: sflow.IfCounters{
					IfIndex:            uint32Field(fields, "if_index"),
					IfType:             uint32Field(fields, "if_type"),
					IfSpeed:            uint64Field(fields, "if_speed"),
					IfDirection:        uint32Field(fields, "if_direction"),
					IfStatus:           uint32Field(fields, "if_status"),
					IfInOctets:         uint64Field(fields, "if_in_octets"),
					IfInUcastPkts:      uint32Field(fields, "if_in_ucast_pkts"),
					IfInMulticastPkts:  uint32Field(fields, "if_in_multicast_pkts"),
					IfInBroadcastPkts:  uint32Field(fields, "if_in_broadcast_pkts"),
					IfInDiscards:       uint32Field(fields, "if_in_discards"),
					IfInErrors:         uint32Field(fields, "if_in_errors"),
					IfInUnknownProtos:  uint32Field(fields, "if_in_unknown_protos"),
					IfOutOctets:        uint64Field(fields, "if_out_octets"),
					IfOutUcastPkts:     uint32Field(fields, "if_out_ucast_pkts"),
					IfOutMulticastPkts: uint32Field(fields, "if_out_multicast_pkts"),
					IfOutBroadcastPkts: uint32Field(fields, "if_out_broadcast_pkts"),
					IfOutDiscards:      uint32Field(fields, "if_out_discards"),
					IfOutErrors:        uint32Field(fields, "if_out_errors"),
					IfPromiscuousMode:  uint32Field(fields, "if_promiscuous_mode"),
				},
			},
		},
	}, nil
}

// counterSampleFormat chooses the sFlow counter record format and source index
// from the canonical event fields.
func (e *SFlowEncoder) counterSampleFormat(fields map[string]any) (uint32, uint32) {
	switch stringFieldOrZero(fields, "counter_format") {
	case "expanded":
		return sflow.SAMPLE_FORMAT_EXPANDED_COUNTER, uint32Field(fields, "source_id_type")
	case "standard":
		return sflow.SAMPLE_FORMAT_COUNTER, 0
	}

	switch e.cfg.CounterFormat {
	case "expanded":
		return sflow.SAMPLE_FORMAT_EXPANDED_COUNTER, uint32Field(fields, "source_id_type")
	default:
		return sflow.SAMPLE_FORMAT_COUNTER, 0
	}
}

type sflowPacketTopLevel struct {
	AgentIP        string
	SubAgentID     uint32
	SequenceNumber uint32
	Uptime         uint32
}

// packetTopLevel extracts the per-datagram sFlow attributes that must match
// across every sample batched into one packet.
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

// sflowAgentIP resolves the emitted agent IP from encoder config first, then event metadata.
func (e *SFlowEncoder) sflowAgentIP(evt *event.Event) string {
	if e.cfg.AgentIP != "" {
		return e.cfg.AgentIP
	}
	if evt.SFlow != nil && evt.SFlow.AgentIP != "" {
		return evt.SFlow.AgentIP
	}
	if agentIP := stringFieldOrZero(evt.Fields, "agent_ip"); agentIP != "" {
		return agentIP
	}
	return "127.0.0.1"
}

// compatibleTopLevel ensures two events can coexist in the same sFlow datagram.
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

// sampleSequence uses an encoder-local counter for outgoing sample ordering.
func (e *SFlowEncoder) sampleSequence(evt *event.Event) uint32 {
	return e.sampleSeq.Add(1)
}

// isSFlowCounterEvent identifies events that should become counter samples
// instead of raw-header flow samples.
func isSFlowCounterEvent(evt *event.Event) bool {
	if evt == nil || evt.Fields == nil {
		return false
	}
	return stringFieldOrZero(evt.Fields, "message_type") == "counter" || stringFieldOrZero(evt.Fields, "record_kind") == "interface_counter"
}

// truncateLastSampleToFit rewrites only the newest sample when that is enough to
// keep the current packet under the datagram limit.
func (e *SFlowEncoder) truncateLastSampleToFit(packet *sflow.Packet) (sflow.FlowSample, bool, error) {
	lastIdx := len(packet.Samples) - 1
	if lastIdx < 0 {
		return sflow.FlowSample{}, false, nil
	}
	sample, ok := packet.Samples[lastIdx].(sflow.FlowSample)
	if !ok || len(sample.Records) != 1 {
		return sflow.FlowSample{}, false, nil
	}
	header, ok := sample.Records[0].Data.(sflow.SampledHeader)
	if !ok || len(header.HeaderData) == 0 {
		return sflow.FlowSample{}, false, nil
	}

	original := append([]byte(nil), header.HeaderData...)
	best := sample
	fit := false
	low, high := 0, len(original)
	for low <= high {
		mid := (low + high) / 2
		candidate := sample
		candidateHeader := header
		candidateHeader.HeaderData = append([]byte(nil), original[:mid]...)
		candidateHeader.OriginalLength = uint32(len(candidateHeader.HeaderData))
		candidate.Records = append([]sflow.FlowRecord(nil), sample.Records...)
		candidate.Records[0] = sflow.FlowRecord{
			Header: sample.Records[0].Header,
			Data:   candidateHeader,
		}
		packet.Samples[lastIdx] = candidate
		data, err := sflow.EncodeMessage(packet)
		if err != nil {
			packet.Samples[lastIdx] = sample
			return sflow.FlowSample{}, false, fmt.Errorf("encode truncated sflow packet: %w", err)
		}
		if len(data) <= e.maxDatagramBytes {
			best = candidate
			fit = true
			low = mid + 1
			continue
		}
		high = mid - 1
	}
	packet.Samples[lastIdx] = sample
	if !fit {
		return sflow.FlowSample{}, false, nil
	}
	return best, true, nil
}

// stringField reads a required string field and reports a type-aware error.
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

// stringFieldOrZero reads an optional string field and returns an empty string when absent.
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

// uint32Field normalizes common integer representations from the generic field map.
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

// uint64Field normalizes common integer representations from the generic field map.
func uint64Field(fields map[string]any, key string) uint64 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	return uint64FromAny(val)
}

// int64Field normalizes common integer representations from the generic field map.
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

// bytesField returns a byte-oriented field in either raw []byte or string form.
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

// sflowSubAgentID prefers explicit event metadata over generic fields.
func sflowSubAgentID(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.SubAgentID != 0 {
		return sf.SubAgentID
	}
	return uint32Field(fields, "sub_agent_id")
}

// sflowSequenceNumber prefers explicit event metadata over encoder-local sequencing.
func sflowSequenceNumber(sf *event.SFlowMetadata) uint32 {
	if sf == nil {
		return 0
	}
	return sf.SequenceNumber
}

// sflowUptime prefers explicit event metadata over encoder-derived uptime.
func sflowUptime(sf *event.SFlowMetadata) uint32 {
	if sf == nil {
		return 0
	}
	return sf.Uptime
}

// sflowSourceID prefers explicit event metadata over generic fields.
func sflowSourceID(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.SourceID != 0 {
		return sf.SourceID
	}
	return uint32Field(fields, "source_id")
}

// sflowSamplingRate prefers explicit event metadata over generic fields.
func sflowSamplingRate(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.SamplingRate != 0 {
		return sf.SamplingRate
	}
	return uint32Field(fields, "sampling_rate")
}

// sflowSamplePool prefers explicit event metadata over generic fields.
func sflowSamplePool(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.SamplePool != 0 {
		return sf.SamplePool
	}
	return uint32Field(fields, "sample_pool")
}

// sflowDrops prefers explicit event metadata over generic fields.
func sflowDrops(sf *event.SFlowMetadata, fields map[string]any) uint32 {
	if sf != nil && sf.Drops != 0 {
		return sf.Drops
	}
	return uint32Field(fields, "drops")
}

// batchOverEnabled defaults unset batch-over toggles to true.
func batchOverEnabled(v *bool) bool {
	return v == nil || *v
}

// logOversizedSample keeps oversize-drop behavior visible without failing the entire pipeline.
func logOversizedSample(err error) {
	var sizeErr *sflowSampleTooLargeError
	if errors.As(err, &sizeErr) {
		slog.Warn(
			"dropping oversized sflow sample",
			slog.Int("max_datagram_bytes", sizeErr.MaxDatagramBytes),
			slog.Int("current_size", sizeErr.CurrentSize),
		)
		return
	}
	slog.Warn("dropping oversized sflow sample", slog.String("error", err.Error()))
}

// encodeIPBytes converts string IPs into the byte-oriented base64 shape used by
// the legacy goflow2v2 JSON payload.
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

// flowTypeField maps string flow-type labels to the integer enum values expected
// by the legacy goflow2v2 JSON layout.
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

// buildPacket translates one runtime event into the appropriate IPFIX packet
// flavor: control/template output or a normal data set.
func (e *IPFIXEncoder) buildPacket(evt *event.Event) (*netflow.IPFIXPacket, error) {
	if evt == nil {
		return nil, fmt.Errorf("nil event")
	}
	if evt.Fields == nil {
		return nil, fmt.Errorf("event fields are empty")
	}

	templateID := uint16Field(evt.Fields, "template_id")
	if templateID == 0 {
		templateID = 256
	}
	obsDomainID := e.observationDomainID(evt.Fields)
	exportTime := uint32(evt.ReceivedAt.Unix())
	if evt.ReceivedAt.IsZero() {
		exportTime = uint32(time.Now().Unix())
	}

	switch evt.Payload.(type) {
	case netflow.TemplateRecord:
		record := evt.Payload.(netflow.TemplateRecord)
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 2},
					Records:       []netflow.TemplateRecord{record},
				},
			},
		}
		return packet, nil
	case *netflow.TemplateRecord:
		record := *evt.Payload.(*netflow.TemplateRecord)
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 2},
					Records:       []netflow.TemplateRecord{record},
				},
			},
		}
		return packet, nil
	case netflow.IPFIXOptionsTemplateRecord:
		record := evt.Payload.(netflow.IPFIXOptionsTemplateRecord)
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.IPFIXOptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 3},
					Records:       []netflow.IPFIXOptionsTemplateRecord{record},
				},
			},
		}
		return packet, nil
	case *netflow.IPFIXOptionsTemplateRecord:
		record := *evt.Payload.(*netflow.IPFIXOptionsTemplateRecord)
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.IPFIXOptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 3},
					Records:       []netflow.IPFIXOptionsTemplateRecord{record},
				},
			},
		}
		return packet, nil
	}

	stream := eventStream(evt, "flow_data")
	if schema, ok := e.dataSchemas[stream]; ok {
		ipv6 := schema.usesIPv6Template(evt.Fields)
		templateRecord := schema.templateForFamily(ipv6)
		dataRecord, err := buildTemplatedValues(e.cfg.TFlowData, evt.Fields, schema.fieldNames, false, ipv6)
		if err != nil {
			return nil, err
		}
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          exportTime,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: obsDomainID,
			FlowSets: []interface{}{
				netflow.DataFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: templateRecord.TemplateId},
					Records:       []netflow.DataRecord{dataRecord},
				},
			},
		}
		e.seq.Add(1)
		return packet, nil
	}

	templateRecord, dataRecord, err := buildTemplatedDataRecord(e.cfg.TFlowData, evt.Fields, templateID, false)
	if err != nil {
		return nil, err
	}
	packet := &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          exportTime,
		SequenceNumber:      e.seq.Load(),
		ObservationDomainId: obsDomainID,
		FlowSets: []interface{}{
			netflow.TemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 2},
				Records:       []netflow.TemplateRecord{templateRecord},
			},
			netflow.DataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: templateRecord.TemplateId},
				Records:       []netflow.DataRecord{dataRecord},
			},
		},
	}
	e.seq.Add(1)
	return packet, nil
}

func (e *NFv9Encoder) buildPacket(evt *event.Event) (*netflow.NFv9Packet, error) {
	if evt == nil {
		return nil, fmt.Errorf("nil event")
	}
	if evt.Fields == nil {
		return nil, fmt.Errorf("event fields are empty")
	}

	templateID := uint16Field(evt.Fields, "template_id")
	if templateID == 0 {
		templateID = 256
	}
	sourceID := uint32Field(evt.Fields, "source_id")
	exportMS := exportUnixMilliseconds(evt.ReceivedAt, evt.Fields)
	unixSeconds := uint32((exportMS + 999) / 1000)
	sysUptime, _, _ := uptimeWindow(exportMS, int64Field(evt.Fields, "start_time_unix"), int64Field(evt.Fields, "end_time_unix"))

	switch payload := evt.Payload.(type) {
	case netflow.TemplateRecord:
		return &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 0},
					Records:       []netflow.TemplateRecord{payload},
				},
			},
		}, nil
	case *netflow.TemplateRecord:
		return &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 0},
					Records:       []netflow.TemplateRecord{*payload},
				},
			},
		}, nil
	case netflow.NFv9OptionsTemplateRecord:
		return &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.NFv9OptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 1},
					Records:       []netflow.NFv9OptionsTemplateRecord{payload},
				},
			},
		}, nil
	case *netflow.NFv9OptionsTemplateRecord:
		return &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.NFv9OptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 1},
					Records:       []netflow.NFv9OptionsTemplateRecord{*payload},
				},
			},
		}, nil
	}

	stream := eventStream(evt, "flow_data")
	if schema, ok := e.dataSchemas[stream]; ok {
		ipv6 := schema.usesIPv6Template(evt.Fields)
		templateRecord := schema.templateForFamily(ipv6)
		dataRecord, err := buildTemplatedValues(e.cfg.TFlowData, evt.Fields, schema.fieldNames, true, ipv6)
		if err != nil {
			return nil, err
		}
		packet := &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   sysUptime,
			UnixSeconds:    unixSeconds,
			SequenceNumber: e.seq.Load(),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.DataFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: templateRecord.TemplateId},
					Records:       []netflow.DataRecord{dataRecord},
				},
			},
		}
		e.seq.Add(1)
		return packet, nil
	}

	templateRecord, dataRecord, err := buildTemplatedDataRecord(e.cfg.TFlowData, evt.Fields, templateID, true)
	if err != nil {
		return nil, err
	}
	packet := &netflow.NFv9Packet{
		Version:        9,
		Count:          2,
		SystemUptime:   sysUptime,
		UnixSeconds:    unixSeconds,
		SequenceNumber: e.seq.Load(),
		SourceId:       sourceID,
		FlowSets: []interface{}{
			netflow.TemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 0},
				Records:       []netflow.TemplateRecord{templateRecord},
			},
			netflow.DataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: templateRecord.TemplateId},
				Records:       []netflow.DataRecord{dataRecord},
			},
		},
	}
	e.seq.Add(1)
	return packet, nil
}

// buildPacket converts one canonical event into a NetFlow v5 export packet.
func (e *NFv5Encoder) buildPacket(evt *event.Event) (*netflowlegacy.PacketNetFlowV5, error) {
	if evt == nil {
		return nil, fmt.Errorf("nil event")
	}
	if evt.Fields == nil {
		return nil, fmt.Errorf("event fields are empty")
	}

	exportMS := exportUnixMilliseconds(evt.ReceivedAt, evt.Fields)
	sysUptime, first, last := uptimeWindow(exportMS, int64Field(evt.Fields, "start_time_unix"), int64Field(evt.Fields, "end_time_unix"))
	exportTime := time.UnixMilli(exportMS).UTC()

	record := netflowlegacy.RecordsNetFlowV5{
		SrcAddr:  mustIPv4Address(evt.Fields, "src_addr"),
		DstAddr:  mustIPv4Address(evt.Fields, "dst_addr"),
		NextHop:  mustIPv4Address(evt.Fields, "next_hop"),
		Input:    uint16Field(evt.Fields, "input_if"),
		Output:   uint16Field(evt.Fields, "output_if"),
		DPkts:    uint32(uint64FromAny(evt.Fields["packets"])),
		DOctets:  uint32(uint64FromAny(evt.Fields["bytes"])),
		First:    first,
		Last:     last,
		SrcPort:  uint16Field(evt.Fields, "src_port"),
		DstPort:  uint16Field(evt.Fields, "dst_port"),
		TCPFlags: uint8(uint32Field(evt.Fields, "tcp_flags")),
		Proto:    uint8(uint32Field(evt.Fields, "proto")),
		Tos:      uint8(uint32Field(evt.Fields, "tos")),
		SrcAS:    uint16Field(evt.Fields, "src_as"),
		DstAS:    uint16Field(evt.Fields, "dst_as"),
		SrcMask:  uint8(uint32Field(evt.Fields, "src_mask")),
		DstMask:  uint8(uint32Field(evt.Fields, "dst_mask")),
	}

	return &netflowlegacy.PacketNetFlowV5{
		Version:          5,
		Count:            1,
		SysUptime:        sysUptime,
		UnixSecs:         uint32(exportTime.Unix()),
		UnixNSecs:        uint32(exportTime.Nanosecond()),
		FlowSequence:     e.seq.Add(1),
		EngineType:       uint8(uint32Field(evt.Fields, "engine_type")),
		EngineId:         uint8(uint32Field(evt.Fields, "engine_id")),
		SamplingInterval: uint16Field(evt.Fields, "sampling_rate"),
		Records:          []netflowlegacy.RecordsNetFlowV5{record},
	}, nil
}

// observationDomainID resolves the IPFIX observation domain from event fields or config.
func (e *IPFIXEncoder) observationDomainID(fields map[string]any) uint32 {
	if e.cfg.ObservationDomainID != 0 {
		return e.cfg.ObservationDomainID
	}
	return uint32Field(fields, "observation_domain_id")
}

// observationDomainID resolves the NetFlow v9 source ID from event fields or config.
func (e *NFv9Encoder) observationDomainID(fields map[string]any) uint32 {
	if e.cfg.ObservationDomainID != 0 {
		return e.cfg.ObservationDomainID
	}
	return uint32Field(fields, "observation_domain_id")
}

// handleControl routes control events into encoder-specific schema/source registration.
func (e *IPFIXEncoder) handleControl(evt *event.Event) ([][]byte, error) {
	switch controlType(evt) {
	case "schema":
		return e.registerSchema(evt)
	case "source_init":
		return e.registerSourceInit(evt)
	default:
		return nil, nil
	}
}

// handleControl routes control events into encoder-specific schema/source registration.
func (e *NFv9Encoder) handleControl(evt *event.Event) ([][]byte, error) {
	switch controlType(evt) {
	case "schema":
		return e.registerSchema(evt)
	case "source_init":
		return e.registerSourceInit(evt)
	default:
		return nil, nil
	}
}

// registerSchema stores aggregation schema state and emits any required template packets.
func (e *IPFIXEncoder) registerSchema(evt *event.Event) ([][]byte, error) {
	schema, ok := evt.Payload.(event.AggregationSchema)
	if !ok {
		if ptr, ok := evt.Payload.(*event.AggregationSchema); ok && ptr != nil {
			schema = *ptr
		} else {
			return nil, nil
		}
	}
	state, err := buildSchemaState(e.cfg.TFlowData, schema, false)
	if err != nil {
		return nil, err
	}
	if state.baseTemplateID == 0 {
		state.baseTemplateID = e.cfg.TemplateBaseID
	}
	if state.ipv4Template.TemplateId == 0 || state.baseTemplateID != state.ipv4Template.TemplateId {
		state, err = buildSchemaStateWithBase(e.cfg.TFlowData, schema, false, state.baseTemplateID)
		if err != nil {
			return nil, err
		}
	}
	e.dataSchemas[eventStream(evt, schema.Stream)] = state
	payloads, err := e.encodeSchemaTemplates(state)
	if err != nil {
		return nil, err
	}
	e.lastTemplateRun = time.Now().UTC()
	return payloads, nil
}

// registerSchema stores aggregation schema state and emits any required template packets.
func (e *NFv9Encoder) registerSchema(evt *event.Event) ([][]byte, error) {
	schema, ok := evt.Payload.(event.AggregationSchema)
	if !ok {
		if ptr, ok := evt.Payload.(*event.AggregationSchema); ok && ptr != nil {
			schema = *ptr
		} else {
			return nil, nil
		}
	}
	state, err := buildSchemaState(e.cfg.TFlowData, schema, true)
	if err != nil {
		return nil, err
	}
	if state.baseTemplateID == 0 {
		state.baseTemplateID = e.cfg.TemplateBaseID
	}
	if state.ipv4Template.TemplateId == 0 || state.baseTemplateID != state.ipv4Template.TemplateId {
		state, err = buildSchemaStateWithBase(e.cfg.TFlowData, schema, true, state.baseTemplateID)
		if err != nil {
			return nil, err
		}
	}
	e.dataSchemas[eventStream(evt, schema.Stream)] = state
	payloads, err := e.encodeSchemaTemplates(state)
	if err != nil {
		return nil, err
	}
	e.lastTemplateRun = time.Now().UTC()
	return payloads, nil
}

// registerSourceInit stores source-scoped exporter metadata and may emit options templates/data.
func (e *IPFIXEncoder) registerSourceInit(evt *event.Event) ([][]byte, error) {
	state := sourceOptionsFromEvent(evt)
	if state.stream == "" {
		state.stream = eventStream(evt, "options_data")
	}
	if state.templateID == 0 {
		state.templateID = e.cfg.OptionsTemplateBaseID
	}
	if e.cfg.ObservationDomainID != 0 {
		state.observationDomainID = e.cfg.ObservationDomainID
	}
	e.sourceOptions[state.stream] = state
	payloads, err := e.encodeSourceOptions(state)
	if err != nil {
		return nil, err
	}
	e.lastOptionsRun = time.Now().UTC()
	return payloads, nil
}

// registerSourceInit stores source-scoped exporter metadata and may emit options templates/data.
func (e *NFv9Encoder) registerSourceInit(evt *event.Event) ([][]byte, error) {
	state := sourceOptionsFromEvent(evt)
	if state.stream == "" {
		state.stream = eventStream(evt, "options_data")
	}
	if state.templateID == 0 {
		state.templateID = e.cfg.OptionsTemplateBaseID
	}
	if e.cfg.ObservationDomainID != 0 {
		state.observationDomainID = e.cfg.ObservationDomainID
	}
	e.sourceOptions[state.stream] = state
	payloads, err := e.encodeSourceOptions(state)
	if err != nil {
		return nil, err
	}
	e.lastOptionsRun = time.Now().UTC()
	return payloads, nil
}

// flushControlPackets emits periodic template/options refresh packets when due.
func (e *IPFIXEncoder) flushControlPackets(now time.Time) ([][]byte, error) {
	var payloads [][]byte
	if e.cfg.TemplateRefresh > 0 && (e.lastTemplateRun.IsZero() || now.Sub(e.lastTemplateRun) >= time.Duration(e.cfg.TemplateRefresh)*time.Millisecond) {
		for _, schema := range e.dataSchemas {
			encoded, err := e.encodeSchemaTemplates(schema)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastTemplateRun = now
	}
	if e.cfg.OptionsRefresh > 0 && (e.lastOptionsRun.IsZero() || now.Sub(e.lastOptionsRun) >= time.Duration(e.cfg.OptionsRefresh)*time.Millisecond) {
		for _, state := range e.sourceOptions {
			encoded, err := e.encodeSourceOptions(state)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastOptionsRun = now
	}
	return payloads, nil
}

// flushControlPackets emits periodic template/options refresh packets when due.
func (e *NFv9Encoder) flushControlPackets(now time.Time) ([][]byte, error) {
	var payloads [][]byte
	if e.cfg.TemplateRefresh > 0 && (e.lastTemplateRun.IsZero() || now.Sub(e.lastTemplateRun) >= time.Duration(e.cfg.TemplateRefresh)*time.Millisecond) {
		for _, schema := range e.dataSchemas {
			encoded, err := e.encodeSchemaTemplates(schema)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastTemplateRun = now
	}
	if e.cfg.OptionsRefresh > 0 && (e.lastOptionsRun.IsZero() || now.Sub(e.lastOptionsRun) >= time.Duration(e.cfg.OptionsRefresh)*time.Millisecond) {
		for _, state := range e.sourceOptions {
			encoded, err := e.encodeSourceOptions(state)
			if err != nil {
				return nil, err
			}
			payloads = append(payloads, encoded...)
		}
		e.lastOptionsRun = now
	}
	return payloads, nil
}

// encodeSchemaTemplates serializes the current stream schema into one or more IPFIX template sets.
func (e *IPFIXEncoder) encodeSchemaTemplates(state templatedSchemaState) ([][]byte, error) {
	now := uint32(time.Now().Unix())
	var out [][]byte
	for _, templateRecord := range state.templates() {
		packet := &netflow.IPFIXPacket{
			Version:             10,
			ExportTime:          now,
			SequenceNumber:      e.seq.Load(),
			ObservationDomainId: e.cfg.ObservationDomainID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 2},
					Records:       []netflow.TemplateRecord{templateRecord},
				},
			},
		}
		data, err := netflow.EncodeMessage(packet)
		if err != nil {
			return nil, fmt.Errorf("encode ipfix schema template: %w", err)
		}
		out = append(out, data)
	}
	return out, nil
}

// encodeSchemaTemplates serializes the current stream schema into one or more NetFlow v9 template sets.
func (e *NFv9Encoder) encodeSchemaTemplates(state templatedSchemaState) ([][]byte, error) {
	nowMS := time.Now().UnixMilli()
	nowSec := uint32((nowMS + 999) / 1000)
	var out [][]byte
	for _, templateRecord := range state.templates() {
		packet := &netflow.NFv9Packet{
			Version:        9,
			Count:          1,
			SystemUptime:   0,
			UnixSeconds:    nowSec,
			SequenceNumber: e.seq.Load(),
			SourceId:       e.cfg.ObservationDomainID,
			FlowSets: []interface{}{
				netflow.TemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 0},
					Records:       []netflow.TemplateRecord{templateRecord},
				},
			},
		}
		data, err := netflow.EncodeMessage(packet)
		if err != nil {
			return nil, fmt.Errorf("encode netflow v9 schema template: %w", err)
		}
		out = append(out, data)
	}
	return out, nil
}

// encodeSourceOptions serializes source-level exporter metadata as IPFIX options records.
func (e *IPFIXEncoder) encodeSourceOptions(state sourceOptionsState) ([][]byte, error) {
	packet := &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          uint32(time.Now().Unix()),
		SequenceNumber:      e.seq.Load(),
		ObservationDomainId: state.observationDomainID,
		FlowSets: []interface{}{
			netflow.IPFIXOptionsTemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 3},
				Records: []netflow.IPFIXOptionsTemplateRecord{
					{
						TemplateId:      state.templateID,
						FieldCount:      2,
						ScopeFieldCount: 1,
						Scopes: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_observationDomainId, Length: 4},
						},
						Options: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_samplingInterval, Length: 4},
						},
					},
				},
			},
			netflow.OptionsDataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: state.templateID},
				Records: []netflow.OptionsDataRecord{
					{
						ScopesValues: []netflow.DataField{
							{Type: netflow.IPFIX_FIELD_observationDomainId, Value: encodeU32(state.sourceID)},
						},
						OptionsValues: []netflow.DataField{
							{Type: netflow.IPFIX_FIELD_samplingInterval, Value: encodeU32(state.samplingRate)},
						},
					},
				},
			},
		},
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode ipfix source options: %w", err)
	}
	return [][]byte{data}, nil
}

// encodeSourceOptions serializes source-level exporter metadata as NetFlow v9 options records.
func (e *NFv9Encoder) encodeSourceOptions(state sourceOptionsState) ([][]byte, error) {
	packet := &netflow.NFv9Packet{
		Version:        9,
		Count:          2,
		SystemUptime:   0,
		UnixSeconds:    uint32(time.Now().Unix()),
		SequenceNumber: e.seq.Load(),
		SourceId:       state.sourceID,
		FlowSets: []interface{}{
			netflow.NFv9OptionsTemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 1},
				Records: []netflow.NFv9OptionsTemplateRecord{
					{
						TemplateId:   state.templateID,
						ScopeLength:  4,
						OptionLength: 4,
						Scopes: []netflow.Field{
							{Type: 1, Length: 4},
						},
						Options: []netflow.Field{
							{Type: netflow.NFV9_FIELD_SAMPLING_INTERVAL, Length: 4},
						},
					},
				},
			},
			netflow.OptionsDataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: state.templateID},
				Records: []netflow.OptionsDataRecord{
					{
						ScopesValues: []netflow.DataField{
							{Type: 1, Value: encodeU32(state.sourceID)},
						},
						OptionsValues: []netflow.DataField{
							{Type: netflow.NFV9_FIELD_SAMPLING_INTERVAL, Value: encodeU32(state.samplingRate)},
						},
					},
				},
			},
		},
	}
	data, err := netflow.EncodeMessage(packet)
	if err != nil {
		return nil, fmt.Errorf("encode netflow v9 source options: %w", err)
	}
	return [][]byte{data}, nil
}

// buildSchemaState prepares the template state for an aggregated stream, defaulting
// the base template ID when the schema did not set one explicitly.
func buildSchemaState(cfg config.TFlowDataConfig, schema event.AggregationSchema, netflowV9 bool) (templatedSchemaState, error) {
	baseTemplateID := schema.BaseTemplateID
	if baseTemplateID == 0 {
		baseTemplateID = 256
	}
	return buildSchemaStateWithBase(cfg, schema, netflowV9, baseTemplateID)
}

// buildSchemaStateWithBase precomputes IPv4 and optional IPv6 template variants
// for one aggregated stream schema.
func buildSchemaStateWithBase(cfg config.TFlowDataConfig, schema event.AggregationSchema, netflowV9 bool, baseTemplateID uint16) (templatedSchemaState, error) {
	stream := schema.Stream
	if stream == "" {
		stream = "flow_data"
	}
	if baseTemplateID == 0 {
		baseTemplateID = 256
	}
	state := templatedSchemaState{
		stream:         stream,
		fieldNames:     append([]string(nil), schema.FieldNames...),
		baseTemplateID: baseTemplateID,
	}
	ipv4Template, err := buildTemplateRecordFromFields(cfg, state.fieldNames, baseTemplateID, netflowV9, false)
	if err != nil {
		return templatedSchemaState{}, err
	}
	state.ipv4Template = ipv4Template
	if hasAddressField(state.fieldNames) {
		ipv6Template, err := buildTemplateRecordFromFields(cfg, state.fieldNames, baseTemplateID+1, netflowV9, true)
		if err != nil {
			return templatedSchemaState{}, err
		}
		state.ipv6Template = ipv6Template
		state.hasIPv6Variant = true
	}
	return state, nil
}

// templateForFields selects the IPv6 variant only when the current event needs it.
func (s templatedSchemaState) templateForFields(fields map[string]any) netflow.TemplateRecord {
	if s.hasIPv6Variant && eventHasIPv6(fields) {
		return s.ipv6Template
	}
	return s.ipv4Template
}

// usesIPv6Template reports whether the event requires the IPv6 schema variant.
func (s templatedSchemaState) usesIPv6Template(fields map[string]any) bool {
	return s.hasIPv6Variant && eventHasIPv6(fields)
}

// templateForFamily selects the prebuilt template by IP family.
func (s templatedSchemaState) templateForFamily(ipv6 bool) netflow.TemplateRecord {
	if ipv6 && s.hasIPv6Variant {
		return s.ipv6Template
	}
	return s.ipv4Template
}

// templates returns every template record that must be announced for this schema.
func (s templatedSchemaState) templates() []netflow.TemplateRecord {
	if s.hasIPv6Variant {
		return []netflow.TemplateRecord{s.ipv4Template, s.ipv6Template}
	}
	return []netflow.TemplateRecord{s.ipv4Template}
}

// sourceOptionsFromEvent extracts source-level exporter metadata from either the
// event payload or its normalized fields.
func sourceOptionsFromEvent(evt *event.Event) sourceOptionsState {
	state := sourceOptionsState{
		stream:              eventStream(evt, "options_data"),
		agentIP:             stringFieldOrZero(evt.Fields, "agent_ip"),
		sourceID:            uint32Field(evt.Fields, "source_id"),
		observationDomainID: uint32Field(evt.Fields, "observation_domain_id"),
		samplingRate:        uint32Field(evt.Fields, "sampling_rate"),
		samplePool:          uint32Field(evt.Fields, "sample_pool"),
		drops:               uint32Field(evt.Fields, "drops"),
		inputIf:             uint32Field(evt.Fields, "input_if"),
		outputIf:            uint32Field(evt.Fields, "output_if"),
	}
	if payload, ok := evt.Payload.(event.SourceInit); ok {
		if payload.Stream != "" {
			state.stream = payload.Stream
		}
		if payload.AgentIP != "" {
			state.agentIP = payload.AgentIP
		}
		if payload.SourceID != 0 {
			state.sourceID = payload.SourceID
		}
		if payload.ObservationDomainID != 0 {
			state.observationDomainID = payload.ObservationDomainID
		}
		if payload.SamplingRate != 0 {
			state.samplingRate = payload.SamplingRate
		}
		if payload.SamplePool != 0 {
			state.samplePool = payload.SamplePool
		}
		if payload.Drops != 0 {
			state.drops = payload.Drops
		}
		if payload.InputIf != 0 {
			state.inputIf = payload.InputIf
		}
		if payload.OutputIf != 0 {
			state.outputIf = payload.OutputIf
		}
	}
	return state
}

// buildTemplatedDataRecord picks fields from a runtime event and builds both the
// template and one matching data record.
func buildTemplatedDataRecord(cfg config.TFlowDataConfig, fieldMap map[string]any, templateID uint16, netflowV9 bool) (netflow.TemplateRecord, netflow.DataRecord, error) {
	names := selectFlowFields(cfg, fieldMap)
	return buildTemplatedDataRecordWithNames(cfg, fieldMap, names, templateID, netflowV9)
}

// buildTemplatedDataRecordWithNames uses an explicit field order, which matters
// when schema events already fixed the record layout.
func buildTemplatedDataRecordWithNames(cfg config.TFlowDataConfig, fieldMap map[string]any, names []string, templateID uint16, netflowV9 bool) (netflow.TemplateRecord, netflow.DataRecord, error) {
	templateFields := make([]netflow.Field, 0, len(names))
	values := make([]netflow.DataField, 0, len(names))
	for _, name := range names {
		def, ok := cfg.Catalog[name]
		if !ok {
			continue
		}
		val, ok := fieldMap[name]
		if !ok {
			continue
		}
		def = resolvedFieldDefinition(name, def, val)
		encoded, err := encodeIPFIXValue(def, val)
		if err != nil {
			return netflow.TemplateRecord{}, netflow.DataRecord{}, fmt.Errorf("encode field %q: %w", name, err)
		}
		fieldType := def.ID
		if netflowV9 {
			fieldType = def.NetFlowV9ID
			if fieldType == 0 {
				fieldType = def.ID
			}
		}
		templateFields = append(templateFields, netflow.Field{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Length:      ipfixFieldLength(def, encoded),
			Pen:         def.PEN,
		})
		values = append(values, netflow.DataField{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Pen:         def.PEN,
			Value:       encoded,
		})
	}
	if len(templateFields) == 0 {
		return netflow.TemplateRecord{}, netflow.DataRecord{}, fmt.Errorf("no encodable fields found for ipfix packet")
	}
	return netflow.TemplateRecord{
			TemplateId: templateID,
			FieldCount: uint16(len(templateFields)),
			Fields:     templateFields,
		}, netflow.DataRecord{
			Values: values,
		}, nil
}

// buildTemplatedValues emits one data record using a preannounced template layout,
// filling missing fields with protocol-appropriate zero values.
func buildTemplatedValues(cfg config.TFlowDataConfig, fieldMap map[string]any, names []string, netflowV9 bool, ipv6 bool) (netflow.DataRecord, error) {
	values := make([]netflow.DataField, 0, len(names))
	for _, name := range names {
		def, ok := cfg.Catalog[name]
		if !ok {
			continue
		}
		def = resolvedFieldDefinitionForFamily(name, def, ipv6)

		val, ok := fieldMap[name]
		var encoded []byte
		var err error
		if ok {
			encoded, err = encodeIPFIXValue(def, val)
			if err != nil {
				return netflow.DataRecord{}, fmt.Errorf("encode field %q: %w", name, err)
			}
		} else {
			encoded, err = defaultEncodedValue(def)
			if err != nil {
				return netflow.DataRecord{}, fmt.Errorf("default field %q: %w", name, err)
			}
		}

		fieldType := def.ID
		if netflowV9 {
			fieldType = def.NetFlowV9ID
			if fieldType == 0 {
				fieldType = def.ID
			}
		}
		values = append(values, netflow.DataField{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Pen:         def.PEN,
			Value:       encoded,
		})
	}
	if len(values) == 0 {
		return netflow.DataRecord{}, fmt.Errorf("no encodable values found for templated packet")
	}
	return netflow.DataRecord{Values: values}, nil
}

// buildTemplateRecordFromFields creates a protocol template record without any data.
func buildTemplateRecordFromFields(cfg config.TFlowDataConfig, names []string, templateID uint16, netflowV9 bool, ipv6 bool) (netflow.TemplateRecord, error) {
	fields := make([]netflow.Field, 0, len(names))
	for _, name := range names {
		def, ok := cfg.Catalog[name]
		if !ok {
			continue
		}
		def = resolvedFieldDefinitionForFamily(name, def, ipv6)
		fieldType := def.ID
		if netflowV9 {
			fieldType = def.NetFlowV9ID
			if fieldType == 0 {
				fieldType = def.ID
			}
		}
		fields = append(fields, netflow.Field{
			PenProvided: def.EnterpriseScoped || def.PEN != 0,
			Type:        fieldType,
			Length:      def.Length,
			Pen:         def.PEN,
		})
	}
	if len(fields) == 0 {
		return netflow.TemplateRecord{}, fmt.Errorf("no encodable fields found for schema template")
	}
	return netflow.TemplateRecord{
		TemplateId: templateID,
		FieldCount: uint16(len(fields)),
		Fields:     fields,
	}, nil
}

// selectFlowFields uses the configured field whitelist when present, otherwise it
// exports all available fields in sorted order for determinism.
func selectFlowFields(cfg config.TFlowDataConfig, fieldMap map[string]any) []string {
	if len(cfg.Select) > 0 {
		return append([]string(nil), cfg.Select...)
	}
	names := make([]string, 0, len(fieldMap))
	for name := range fieldMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// encodeIPFIXValue encodes one canonical field into the wire representation
// expected by IPFIX or NetFlow v9.
func encodeIPFIXValue(def config.IPFIXFieldDefinition, val any) ([]byte, error) {
	switch def.Type {
	case "ipv4Address", "ipv6Address":
		s, ok := val.(string)
		if !ok {
			return nil, fmt.Errorf("expected string IP, got %T", val)
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return nil, err
		}
		if def.Type == "ipv4Address" {
			if !addr.Is4() {
				return nil, fmt.Errorf("expected IPv4 address, got %s", s)
			}
			return append([]byte(nil), addr.AsSlice()...), nil
		}
		if !addr.Is6() {
			return nil, fmt.Errorf("expected IPv6 address, got %s", s)
		}
		return append([]byte(nil), addr.AsSlice()...), nil
	case "unsigned8", "unsigned16", "unsigned32", "unsigned64":
		return encodeUnsigned(def.Type, val)
	case "signed8", "signed16", "signed32", "signed64":
		return encodeSigned(def.Type, val)
	case "string":
		switch v := val.(type) {
		case string:
			return []byte(v), nil
		case []byte:
			return append([]byte(nil), v...), nil
		default:
			return nil, fmt.Errorf("expected string/[]byte, got %T", val)
		}
	default:
		switch v := val.(type) {
		case []byte:
			return append([]byte(nil), v...), nil
		case string:
			return []byte(v), nil
		default:
			return encodeUnsigned("unsigned64", val)
		}
	}
}

// defaultEncodedValue provides a zero representation for fields omitted from a
// templated event but still required by the selected template.
func defaultEncodedValue(def config.IPFIXFieldDefinition) ([]byte, error) {
	switch def.Type {
	case "ipv4Address":
		return make([]byte, 4), nil
	case "ipv6Address":
		return make([]byte, 16), nil
	case "unsigned8", "signed8":
		return make([]byte, 1), nil
	case "unsigned16", "signed16":
		return make([]byte, 2), nil
	case "unsigned32", "signed32":
		return make([]byte, 4), nil
	case "unsigned64", "signed64":
		return make([]byte, 8), nil
	case "string":
		if def.Length == 0xffff || def.Length == 0 {
			return []byte{}, nil
		}
		return make([]byte, def.Length), nil
	default:
		if def.Length == 0xffff || def.Length == 0 {
			return []byte{}, nil
		}
		return make([]byte, def.Length), nil
	}
}

// resolvedFieldDefinition upgrades src_addr/dst_addr to their IPv6 definitions
// when the concrete runtime value contains an IPv6 address.
func resolvedFieldDefinition(name string, def config.IPFIXFieldDefinition, val any) config.IPFIXFieldDefinition {
	ipStr, ok := val.(string)
	if !ok {
		return def
	}
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return def
	}
	switch name {
	case "src_addr":
		if addr.Is6() {
			def.Name = "sourceIPv6Address"
			def.ID = netflow.IPFIX_FIELD_sourceIPv6Address
			def.NetFlowV9ID = netflow.NFV9_FIELD_IPV6_SRC_ADDR
			def.Length = 16
			def.Type = "ipv6Address"
		}
	case "dst_addr":
		if addr.Is6() {
			def.Name = "destinationIPv6Address"
			def.ID = netflow.IPFIX_FIELD_destinationIPv6Address
			def.NetFlowV9ID = netflow.NFV9_FIELD_IPV6_DST_ADDR
			def.Length = 16
			def.Type = "ipv6Address"
		}
	}
	return def
}

// resolvedFieldDefinitionForFamily performs the same promotion as
// resolvedFieldDefinition, but from a preselected IP family.
func resolvedFieldDefinitionForFamily(name string, def config.IPFIXFieldDefinition, ipv6 bool) config.IPFIXFieldDefinition {
	if !ipv6 {
		return def
	}
	switch name {
	case "src_addr":
		def.Name = "sourceIPv6Address"
		def.ID = netflow.IPFIX_FIELD_sourceIPv6Address
		def.NetFlowV9ID = netflow.NFV9_FIELD_IPV6_SRC_ADDR
		def.Length = 16
		def.Type = "ipv6Address"
	case "dst_addr":
		def.Name = "destinationIPv6Address"
		def.ID = netflow.IPFIX_FIELD_destinationIPv6Address
		def.NetFlowV9ID = netflow.NFV9_FIELD_IPV6_DST_ADDR
		def.Length = 16
		def.Type = "ipv6Address"
	}
	return def
}

// hasAddressField reports whether a schema needs dual IPv4/IPv6 template support.
func hasAddressField(names []string) bool {
	for _, name := range names {
		if name == "src_addr" || name == "dst_addr" {
			return true
		}
	}
	return false
}

// eventHasIPv6 checks the common address fields to determine which template family to use.
func eventHasIPv6(fields map[string]any) bool {
	for _, key := range []string{"src_addr", "dst_addr"} {
		ip := stringFieldOrZero(fields, key)
		if ip == "" {
			continue
		}
		addr, err := netip.ParseAddr(ip)
		if err == nil && addr.Is6() {
			return true
		}
	}
	return false
}

// eventStream prefers the explicit event stream, then control stream, then a caller fallback.
func eventStream(evt *event.Event, fallback string) string {
	if evt != nil && evt.Stream != "" {
		return evt.Stream
	}
	if evt != nil && evt.Control != nil && evt.Control.Stream != "" {
		return evt.Control.Stream
	}
	return fallback
}

// controlType safely returns the event control type when present.
func controlType(evt *event.Event) string {
	if evt == nil || evt.Control == nil {
		return ""
	}
	return evt.Control.Type
}

// encodeU32 writes one uint32 in big-endian order.
func encodeU32(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

// ipfixFieldLength honors explicit field lengths and falls back to encoded size
// for variable-length definitions.
func ipfixFieldLength(def config.IPFIXFieldDefinition, encoded []byte) uint16 {
	if def.Length != 0 {
		return def.Length
	}
	if len(encoded) > 65535 {
		return 0xffff
	}
	return uint16(len(encoded))
}

// encodeUnsigned serializes unsigned integer field kinds in big-endian order.
func encodeUnsigned(kind string, val any) ([]byte, error) {
	v := uint64FromAny(val)
	switch kind {
	case "unsigned8":
		return []byte{byte(v)}, nil
	case "unsigned16":
		return []byte{byte(v >> 8), byte(v)}, nil
	case "unsigned32":
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}, nil
	default:
		return []byte{
			byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
			byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
		}, nil
	}
}

// encodeSigned serializes signed integer field kinds in big-endian order.
func encodeSigned(kind string, val any) ([]byte, error) {
	v := int64Field(map[string]any{"v": val}, "v")
	switch kind {
	case "signed8":
		return []byte{byte(v)}, nil
	case "signed16":
		return []byte{byte(v >> 8), byte(v)}, nil
	case "signed32":
		return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}, nil
	default:
		return []byte{
			byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
			byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
		}, nil
	}
}

// uint16Field is a convenience wrapper around the generic uint32 field reader.
func uint16Field(fields map[string]any, key string) uint16 {
	return uint16(uint32Field(fields, key))
}

// exportUnixMilliseconds picks the best available export timestamp for encoders
// that need an absolute export time even when flow timings are absent.
func exportUnixMilliseconds(receivedAt time.Time, fields map[string]any) int64 {
	endMS := int64Field(fields, "end_time_unix")
	if endMS > 0 {
		return endMS
	}
	startMS := int64Field(fields, "start_time_unix")
	if startMS > 0 {
		return startMS
	}
	if !receivedAt.IsZero() {
		return receivedAt.UnixMilli()
	}
	return time.Now().UnixMilli()
}

// uptimeWindow derives the relative uptime values expected by NetFlow v5 from
// absolute millisecond timestamps.
func uptimeWindow(exportMS, startMS, endMS int64) (sysUptime, first, last uint32) {
	if startMS <= 0 {
		startMS = exportMS
	}
	if endMS <= 0 {
		endMS = exportMS
	}
	baseMS := exportMS
	if startMS < baseMS {
		baseMS = startMS
	}
	if endMS < baseMS {
		baseMS = endMS
	}
	if exportMS < baseMS {
		baseMS = exportMS
	}
	return uint32(exportMS - baseMS), uint32(startMS - baseMS), uint32(endMS - baseMS)
}

// mustIPv4Address parses an IPv4 string field into the legacy NetFlow v5 integer form.
func mustIPv4Address(fields map[string]any, key string) netflowlegacy.IPAddress {
	ip := stringFieldOrZero(fields, key)
	if ip == "" {
		return 0
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return 0
	}
	raw := addr.As4()
	return netflowlegacy.IPAddress(uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3]))
}

// uint64FromAny normalizes several integer representations into uint64 for encoders.
func uint64FromAny(val any) uint64 {
	switch v := val.(type) {
	case uint64:
		return v
	case uint32:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint8:
		return uint64(v)
	case int64:
		return uint64(v)
	case int:
		return uint64(v)
	case float64:
		return uint64(v)
	default:
		return 0
	}
}
