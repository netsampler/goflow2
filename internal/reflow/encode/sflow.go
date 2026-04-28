package encode

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/packet"
)

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

type SFlowEncoder struct {
	packetSeq        atomic.Uint32
	sampleSeq        atomic.Uint32
	started          time.Time
	maxDatagramBytes int
	allowTruncate    bool
	batch            config.BatchConfig
	cfg              config.SFlowConfig
	events           []*event.Event
	estimatedBytes   int
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
		e.appendEvent(evt)
		if e.shouldFlush() {
			flushed, err := e.Flush()
			if err != nil {
				return nil, err
			}
			return append(payloads, flushed...), nil
		}
		return payloads, nil
	}

	e.appendEvent(evt)
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
	e.estimatedBytes = 0

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
	if e.batch.MaxBytes > 0 && e.estimatedBytes >= e.batch.MaxBytes {
		return true
	}
	return false
}

func (e *SFlowEncoder) appendEvent(evt *event.Event) {
	e.events = append(e.events, evt)
	e.estimatedBytes += estimatedEventSize(evt)
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
	headerData, protocol, frameLength, originalLength := e.sampledHeaderFields(evt, fields)

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
					Protocol:       protocol,
					FrameLength:    frameLength,
					Stripped:       uint32Field(fields, "stripped"),
					OriginalLength: originalLength,
					HeaderData:     headerData,
				},
			},
		},
	}, nil
}

func (e *SFlowEncoder) sampledHeaderFields(evt *event.Event, fields map[string]any) ([]byte, uint32, uint32, uint32) {
	headerData := bytesField(fields, "header_data")
	protocol := uint32Field(fields, "protocol")
	frameLength := uint32Field(fields, "frame_length")
	originalLength := uint32Field(fields, "original_length")
	if len(headerData) == 0 {
		if pseudoHeader, ok := packet.BuildPseudoHeader(evt, fields); ok {
			headerData = pseudoHeader
			if protocol == 0 {
				protocol = sampledHeaderProtocolForPacket(evt, headerData)
			}
			if frameLength == 0 {
				frameLength = uint32(len(headerData))
			}
			if originalLength == 0 {
				originalLength = uint32(len(headerData))
			}
		}
	}
	return headerData, protocol, frameLength, originalLength
}

func sampledHeaderProtocolForPacket(evt *event.Event, headerData []byte) uint32 {
	if evt != nil && evt.Packet != nil && len(evt.Packet.Layers) > 0 {
		switch evt.Packet.Layers[0].Kind {
		case "ipv4":
			return 11
		case "ipv6":
			return 12
		default:
			return 1
		}
	}
	if len(headerData) > 0 {
		switch headerData[0] >> 4 {
		case 4:
			return 11
		case 6:
			return 12
		}
	}
	return 1
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
		AgentIP:    e.sflowAgentIP(evt),
		SubAgentID: sflowSubAgentID(evt.SFlow, evt.Fields),
		Uptime:     sflowUptime(evt.SFlow),
	}
	if e.cfg.UseMetadataSequenceNumber {
		top.SequenceNumber = sflowSequenceNumber(evt.SFlow)
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
	return stringFieldOrZero(evt.Fields, "record_kind") == "interface_counter"
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
