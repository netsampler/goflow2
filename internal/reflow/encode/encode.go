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

func (e *sflowSampleTooLargeError) Error() string {
	return fmt.Sprintf("%s: current_size=%d max_datagram_bytes=%d", ErrSFlowSampleTooLarge, e.CurrentSize, e.MaxDatagramBytes)
}

func (e *sflowSampleTooLargeError) Unwrap() error {
	return ErrSFlowSampleTooLarge
}

// New builds the configured encoder. Each encoder worker gets its own instance.
func New(cfg config.EncoderConfig) (Encoder, error) {
	switch cfg.Type {
	case "", "json":
		return NewJSONEncoder(cfg), nil
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
	seq atomic.Uint32
	cfg config.EncoderConfig
}

type NFv9Encoder struct {
	seq atomic.Uint32
	cfg config.EncoderConfig
}

type NFv5Encoder struct {
	seq atomic.Uint32
}

func NewIPFIXEncoder(cfg config.EncoderConfig) *IPFIXEncoder {
	return &IPFIXEncoder{cfg: cfg}
}

func (e *IPFIXEncoder) Encode(evt *event.Event) ([][]byte, error) {
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
	return nil, nil
}

func NewNFv9Encoder(cfg config.EncoderConfig) *NFv9Encoder {
	return &NFv9Encoder{cfg: cfg}
}

func (e *NFv9Encoder) Encode(evt *event.Event) ([][]byte, error) {
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
	return nil, nil
}

func NewNFv5Encoder(cfg config.EncoderConfig) *NFv5Encoder {
	return &NFv5Encoder{}
}

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

func (e *NFv5Encoder) Flush() ([][]byte, error) {
	return nil, nil
}

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
		sample, err := e.buildFlowSample(evt)
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
	if agentIP := stringFieldOrZero(evt.Fields, "agent_ip"); agentIP != "" {
		return agentIP
	}
	return "127.0.0.1"
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
	return e.sampleSeq.Add(1)
}

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
	obsDomainID := uint32Field(evt.Fields, "source_id")
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
			SequenceNumber:      e.seq.Add(1),
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
			SequenceNumber:      e.seq.Add(1),
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
			SequenceNumber:      e.seq.Add(1),
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
			SequenceNumber:      e.seq.Add(1),
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

	templateRecord, dataRecord, err := buildTemplatedDataRecord(e.cfg.TFlowData, evt.Fields, templateID, false)
	if err != nil {
		return nil, err
	}
	packet := &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          exportTime,
		SequenceNumber:      e.seq.Add(1),
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
			SequenceNumber: e.seq.Add(1),
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
			SequenceNumber: e.seq.Add(1),
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
			SequenceNumber: e.seq.Add(1),
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
			SequenceNumber: e.seq.Add(1),
			SourceId:       sourceID,
			FlowSets: []interface{}{
				netflow.NFv9OptionsTemplateFlowSet{
					FlowSetHeader: netflow.FlowSetHeader{Id: 1},
					Records:       []netflow.NFv9OptionsTemplateRecord{*payload},
				},
			},
		}, nil
	}

	templateRecord, dataRecord, err := buildTemplatedDataRecord(e.cfg.TFlowData, evt.Fields, templateID, true)
	if err != nil {
		return nil, err
	}
	return &netflow.NFv9Packet{
		Version:        9,
		Count:          2,
		SystemUptime:   sysUptime,
		UnixSeconds:    unixSeconds,
		SequenceNumber: e.seq.Add(1),
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
	}, nil
}

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

func buildTemplatedDataRecord(cfg config.TFlowDataConfig, fieldMap map[string]any, templateID uint16, netflowV9 bool) (netflow.TemplateRecord, netflow.DataRecord, error) {
	names := selectFlowFields(cfg, fieldMap)
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

func ipfixFieldLength(def config.IPFIXFieldDefinition, encoded []byte) uint16 {
	if def.Length != 0 {
		return def.Length
	}
	if len(encoded) > 65535 {
		return 0xffff
	}
	return uint16(len(encoded))
}

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

func uint16Field(fields map[string]any, key string) uint16 {
	return uint16(uint32Field(fields, key))
}

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
