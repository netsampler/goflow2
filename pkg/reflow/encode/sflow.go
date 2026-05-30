package encode

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/packet"
)

var ErrSFlowSampleTooLarge = errors.New("sflow sample exceeds max_datagram_bytes")

const sflowFlowRecordsInternalKey = "sflow_flow_records"
const sflowSampleInternalKey = "sflow_sample"
const sflowCounterRecordsInternalKey = "sflow_counter_records"

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
	maxHeaderBytes   int
	batch            config.BatchConfig
	cfg              config.SFlowConfig
	events           []*event.Event
}

// NewSFlowEncoder builds one encoder instance per runtime worker. Each instance
// keeps its own batch buffer and sequence counters.
func NewSFlowEncoder(cfg config.EncoderConfig) *SFlowEncoder {
	return &SFlowEncoder{
		started:          time.Now(),
		maxDatagramBytes: cfg.MaxDatagramBytes,
		allowTruncate:    cfg.AllowTruncate != nil && *cfg.AllowTruncate,
		maxHeaderBytes:   cfg.SFlow.MaxHeaderBytes,
		batch:            cfg.Batch,
		cfg:              cfg.SFlow,
	}
}

// Encode appends an event to the encoder-local batch or encodes it immediately.
func (e *SFlowEncoder) Encode(evt *event.Event) ([][]byte, error) {
	if evt == nil || evt.Kind == "control" {
		return nil, nil
	}

	if !e.batch.IsEnabled() {
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
	return false
}

func (e *SFlowEncoder) appendEvent(evt *event.Event) {
	e.events = append(e.events, evt)
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
		if limit := e.batchDatagramLimit(); limit > 0 {
			data, err := sflow.EncodeMessage(packet)
			if err != nil {
				return nil, accepted, fmt.Errorf("encode sflow packet: %w", err)
			}
			lastSize = len(data)
			if len(data) > limit {
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
						MaxDatagramBytes: limit,
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
			MaxDatagramBytes: e.batchDatagramLimit(),
			CurrentSize:      0,
		}
	}

	packet.SamplesCount = uint32(len(packet.Samples))
	return packet, accepted, nil
}

func (e *SFlowEncoder) batchDatagramLimit() int {
	limit := e.maxDatagramBytes
	if e.batch.IsEnabled() && e.batch.MaxBytes > 0 && (limit <= 0 || e.batch.MaxBytes < limit) {
		limit = e.batch.MaxBytes
	}
	return limit
}

// buildSample dispatches between flow-sample and counter-sample output.
func (e *SFlowEncoder) buildSample(evt *event.Event) (interface{}, error) {
	if sample, ok := evt.Internal[sflowSampleInternalKey].(sflow.RawSample); ok {
		return sample, nil
	}
	if sample, ok := evt.Internal[sflowSampleInternalKey].(*sflow.RawSample); ok {
		return sample, nil
	}
	if sample, ok := publicSFlowRawSample(evt); ok {
		return sample, nil
	}
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
	headerData, protocol, frameLength, originalLength := e.sampledHeaderFields(evt, fields)

	records := []sflow.FlowRecord{
		{
			Data: sflow.SampledHeader{
				Protocol:       protocol,
				FrameLength:    frameLength,
				Stripped:       uint32Field(fields, "stripped"),
				OriginalLength: originalLength,
				HeaderData:     headerData,
			},
		},
	}
	records = append(records, preservedSFlowFlowRecords(evt)...)
	records = append(records, publicSFlowFlowRecords(evt)...)
	if e.emitExtendedRecords() {
		records = append(records, e.syntheticExtendedRecords(fields, records)...)
	}

	return sflow.FlowSample{
		Header: sflow.SampleHeader{
			Format:               sflow.SAMPLE_FORMAT_FLOW,
			SampleSequenceNumber: e.sampleSequence(evt),
			SourceIdType:         0,
			SourceIdValue:        sflowSourceID(evt),
		},
		SamplingRate: sflowSamplingRate(evt),
		SamplePool:   sflowSamplePool(evt),
		Drops:        sflowDrops(evt),
		Input:        uint32Field(fields, "input_if"),
		Output:       uint32Field(fields, "output_if"),
		Records:      records,
	}, nil
}

func (e *SFlowEncoder) emitExtendedRecords() bool {
	return e.cfg.EmitExtendedRecords == nil || *e.cfg.EmitExtendedRecords
}

func preservedSFlowFlowRecords(evt *event.Event) []sflow.FlowRecord {
	if evt == nil || evt.Internal == nil {
		return nil
	}
	records, _ := evt.Internal[sflowFlowRecordsInternalKey].([]sflow.FlowRecord)
	if len(records) == 0 {
		return nil
	}
	out := make([]sflow.FlowRecord, len(records))
	copy(out, records)
	return out
}

func publicSFlowRawSample(evt *event.Event) (sflow.RawSample, bool) {
	if evt == nil || evt.SFlow == nil || len(evt.SFlow.Samples) == 0 {
		return sflow.RawSample{}, false
	}
	sample := evt.SFlow.Samples[0]
	if len(sample.Data) == 0 {
		return sflow.RawSample{}, false
	}
	dataFormat, ok := publicSFlowDataFormat(sample.Enterprise, sample.Format, sample.DataFormat)
	if !ok {
		return sflow.RawSample{}, false
	}
	if !validPublicRawSamplePayload(dataFormat, sample.Data) {
		return sflow.RawSample{}, false
	}
	return sflow.RawSample{
		Header: sflow.SampleHeader{Format: dataFormat},
		Data:   append([]byte(nil), sample.Data...),
	}, true
}

func validPublicRawSamplePayload(dataFormat uint32, data []byte) bool {
	if sflow.DataFormatEnterprise(dataFormat) != 0 {
		return true
	}
	switch sflow.DataFormatFormat(dataFormat) {
	case sflow.SAMPLE_FORMAT_FLOW, sflow.SAMPLE_FORMAT_COUNTER, sflow.SAMPLE_FORMAT_EXPANDED_FLOW, sflow.SAMPLE_FORMAT_EXPANDED_COUNTER, sflow.SAMPLE_FORMAT_DROP:
		header := sflow.SampleHeader{Format: dataFormat, Length: uint32(len(data))}
		payload := bytes.NewBuffer(data)
		if _, err := sflow.DecodeSample(&header, payload); err != nil {
			return false
		}
		return payload.Len() == 0
	default:
		return true
	}
}

func publicSFlowFlowRecords(evt *event.Event) []sflow.FlowRecord {
	if evt == nil || evt.SFlow == nil || len(evt.SFlow.Samples) == 0 {
		return nil
	}
	rawRecords := evt.SFlow.Samples[0].RawFlowRecords
	records := make([]sflow.FlowRecord, 0, len(rawRecords))
	for _, raw := range rawRecords {
		dataFormat, ok := publicSFlowDataFormat(raw.Enterprise, raw.Format, raw.DataFormat)
		if !ok {
			continue
		}
		records = append(records, sflow.FlowRecord{
			Header: sflow.RecordHeader{DataFormat: dataFormat},
			Data:   sflow.RawRecord{Data: append([]byte(nil), raw.Data...)},
		})
	}
	return records
}

func publicSFlowDataFormat(enterprise, format, dataFormat uint32) (uint32, bool) {
	if dataFormat != 0 {
		return dataFormat, true
	}
	if enterprise == 0 && format == 0 {
		return 0, false
	}
	return sflow.PackDataFormat(enterprise, format), true
}

func (e *SFlowEncoder) syntheticExtendedRecords(fields map[string]any, existing []sflow.FlowRecord) []sflow.FlowRecord {
	var records []sflow.FlowRecord
	if !hasStandardSFlowRecord(existing, sflow.FLOW_TYPE_EXT_NAT) {
		if record, ok := syntheticNATRecord(fields); ok {
			records = append(records, record)
		}
	}
	if !hasStandardSFlowRecord(existing, sflow.FLOW_TYPE_EXT_MPLS) {
		if record, ok := syntheticMPLSRecord(fields); ok {
			records = append(records, record)
		}
	}
	if !hasStandardSFlowRecord(existing, sflow.FLOW_TYPE_EXT_MPLS_TUNNEL) {
		if record, ok := syntheticMPLSTunnelRecord(fields); ok {
			records = append(records, record)
		}
	}
	if !hasStandardSFlowRecord(existing, sflow.FLOW_TYPE_EXT_MPLS_VC) {
		if record, ok := syntheticMPLSVCRecord(fields); ok {
			records = append(records, record)
		}
	}
	if !hasStandardSFlowRecord(existing, sflow.FLOW_TYPE_EXT_MPLS_FEC) {
		if record, ok := syntheticMPLSFTNRecord(fields); ok {
			records = append(records, record)
		}
	}
	if !hasStandardSFlowRecord(existing, sflow.FLOW_TYPE_EXT_MPLS_LVP_FEC) {
		if record, ok := syntheticMPLSLDPFECRecord(fields); ok {
			records = append(records, record)
		}
	}
	return records
}

func hasStandardSFlowRecord(records []sflow.FlowRecord, format uint32) bool {
	for _, record := range records {
		dataFormat := flowRecordDataFormat(record)
		if sflow.DataFormatEnterprise(dataFormat) == 0 && sflow.DataFormatFormat(dataFormat) == format {
			return true
		}
	}
	return false
}

func flowRecordDataFormat(record sflow.FlowRecord) uint32 {
	if record.Header.DataFormat != 0 {
		return record.Header.DataFormat
	}
	switch record.Data.(type) {
	case sflow.ExtendedNAT, *sflow.ExtendedNAT:
		return sflow.FLOW_TYPE_EXT_NAT
	case sflow.ExtendedMPLS, *sflow.ExtendedMPLS:
		return sflow.FLOW_TYPE_EXT_MPLS
	case sflow.ExtendedMPLSTunnel, *sflow.ExtendedMPLSTunnel:
		return sflow.FLOW_TYPE_EXT_MPLS_TUNNEL
	case sflow.ExtendedMPLSVC, *sflow.ExtendedMPLSVC:
		return sflow.FLOW_TYPE_EXT_MPLS_VC
	case sflow.ExtendedMPLSFTN, *sflow.ExtendedMPLSFTN:
		return sflow.FLOW_TYPE_EXT_MPLS_FEC
	case sflow.ExtendedMPLSLDPFEC, *sflow.ExtendedMPLSLDPFEC:
		return sflow.FLOW_TYPE_EXT_MPLS_LVP_FEC
	default:
		return 0
	}
}

func syntheticNATRecord(fields map[string]any) (sflow.FlowRecord, bool) {
	src := stringFieldOrZero(fields, "nat_src_addr")
	dst := stringFieldOrZero(fields, "nat_dst_addr")
	if src == "" && dst == "" {
		return sflow.FlowRecord{}, false
	}
	if src == "" {
		src = stringFieldOrZero(fields, "src_addr")
	}
	if dst == "" {
		dst = stringFieldOrZero(fields, "dst_addr")
	}
	srcAddr, srcOK := parseSFlowIP(src)
	dstAddr, dstOK := parseSFlowIP(dst)
	if !srcOK || !dstOK {
		return sflow.FlowRecord{}, false
	}
	return sflow.FlowRecord{Data: sflow.ExtendedNAT{
		SrcAddress: srcAddr,
		DstAddress: dstAddr,
	}}, true
}

func syntheticMPLSRecord(fields map[string]any) (sflow.FlowRecord, bool) {
	inStack := uint32SliceField(fields, "mpls_in_label_stack")
	outStack := uint32SliceField(fields, "mpls_out_label_stack")
	if len(inStack) == 0 {
		inStack = mplsStackFromHelperFields(fields)
	}
	if len(inStack) == 0 && len(outStack) == 0 {
		return sflow.FlowRecord{}, false
	}
	nextHop, _ := parseSFlowIP(stringFieldOrZero(fields, "mpls_next_hop_addr"))
	return sflow.FlowRecord{Data: sflow.ExtendedMPLS{
		NextHop:       nextHop,
		InLabelStack:  inStack,
		OutLabelStack: outStack,
	}}, true
}

func syntheticMPLSTunnelRecord(fields map[string]any) (sflow.FlowRecord, bool) {
	name := stringFieldOrZero(fields, "mpls_tunnel_lsp_name")
	id := uint32Field(fields, "mpls_tunnel_id")
	cos := uint32Field(fields, "mpls_tunnel_cos")
	if name == "" && id == 0 && cos == 0 {
		return sflow.FlowRecord{}, false
	}
	return sflow.FlowRecord{Data: sflow.ExtendedMPLSTunnel{
		TunnelLSPName: name,
		TunnelID:      id,
		TunnelCOS:     cos,
	}}, true
}

func syntheticMPLSVCRecord(fields map[string]any) (sflow.FlowRecord, bool) {
	name := stringFieldOrZero(fields, "mpls_vc_instance_name")
	id := uint32Field(fields, "mpls_vll_vc_id")
	cos := uint32Field(fields, "mpls_vc_label_cos")
	if name == "" && id == 0 && cos == 0 {
		return sflow.FlowRecord{}, false
	}
	return sflow.FlowRecord{Data: sflow.ExtendedMPLSVC{
		VCInstanceName: name,
		VLLVCID:        id,
		VCLabelCOS:     cos,
	}}, true
}

func syntheticMPLSFTNRecord(fields map[string]any) (sflow.FlowRecord, bool) {
	descr := stringFieldOrZero(fields, "mpls_ftn_descr")
	mask := uint32Field(fields, "mpls_ftn_mask")
	if descr == "" && mask == 0 {
		return sflow.FlowRecord{}, false
	}
	return sflow.FlowRecord{Data: sflow.ExtendedMPLSFTN{
		MPLSFTNDescr: descr,
		MPLSFTNMask:  mask,
	}}, true
}

func syntheticMPLSLDPFECRecord(fields map[string]any) (sflow.FlowRecord, bool) {
	prefixLength := uint32Field(fields, "mpls_fec_addr_prefix_length")
	if prefixLength == 0 {
		return sflow.FlowRecord{}, false
	}
	return sflow.FlowRecord{Data: sflow.ExtendedMPLSLDPFEC{
		MPLSFecAddrPrefixLength: prefixLength,
	}}, true
}

func parseSFlowIP(value string) ([]byte, bool) {
	if value == "" {
		return nil, true
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return nil, false
	}
	return append([]byte(nil), addr.AsSlice()...), true
}

func uint32SliceField(fields map[string]any, key string) []uint32 {
	if fields == nil {
		return nil
	}
	val, ok := fields[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []uint32:
		return append([]uint32(nil), v...)
	case []any:
		out := make([]uint32, 0, len(v))
		for _, item := range v {
			out = append(out, uint32(uint64FromAny(item)))
		}
		return out
	default:
		return nil
	}
}

func mplsStackFromHelperFields(fields map[string]any) []uint32 {
	var entries []uint32
	for i := 1; i <= 16; i++ {
		section := bytesField(fields, fmt.Sprintf("mpls_label_stack_section_%d", i))
		if len(section) >= 3 {
			entries = append(entries, (uint32(section[0])<<24)|(uint32(section[1])<<16)|(uint32(section[2])<<8))
			continue
		}
		label := uint32Field(fields, fmt.Sprintf("mpls_label_%d", i))
		if label == 0 {
			label = uint32Field(fields, fmt.Sprintf("mpls_label%d", i))
		}
		if label == 0 {
			continue
		}
		entries = append(entries, (label&0xfffff)<<12)
	}
	if len(entries) > 0 {
		entries[len(entries)-1] |= 1 << 8
	}
	return entries
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
	if e.allowTruncate && e.maxHeaderBytes > 0 && len(headerData) > e.maxHeaderBytes {
		headerData = append([]byte(nil), headerData[:e.maxHeaderBytes]...)
		originalLength = uint32(len(headerData))
	}
	if uint32(len(headerData)) != originalLength {
		originalLength = uint32(len(headerData))
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
		if len(publicSFlowCounterRecords(evt)) == 0 && len(preservedSFlowCounterRecords(evt)) == 0 {
			return sflow.CounterSample{}, fmt.Errorf("event fields are empty")
		}
		fields = map[string]any{}
	}
	format, sourceIDType := e.counterSampleFormat(fields)
	records, err := e.counterRecords(evt, fields)
	if err != nil {
		return sflow.CounterSample{}, err
	}

	return sflow.CounterSample{
		Header: sflow.SampleHeader{
			Format:               format,
			SampleSequenceNumber: e.sampleSequence(evt),
			SourceIdType:         sourceIDType,
			SourceIdValue:        sflowSourceID(evt),
		},
		CounterRecordsCount: uint32(len(records)),
		Records:             records,
	}, nil
}

func (e *SFlowEncoder) counterRecords(evt *event.Event, fields map[string]any) ([]sflow.CounterRecord, error) {
	if records := preservedSFlowCounterRecords(evt); len(records) > 0 {
		return records, nil
	}
	publicRecords := publicSFlowCounterRecords(evt)
	if len(publicRecords) > 0 && stringFieldOrZero(fields, "record_kind") == "" {
		return publicRecords, nil
	}

	switch stringFieldOrZero(fields, "record_kind") {
	case "interface_counter":
		records := []sflow.CounterRecord{{Data: sflow.IfCounters{
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
		}}}
		return append(records, publicRecords...), nil
	case "ethernet_counter":
		records := []sflow.CounterRecord{{Data: sflow.EthernetCounters{
			Dot3StatsAlignmentErrors:           uint32Field(fields, "dot3_stats_alignment_errors"),
			Dot3StatsFCSErrors:                 uint32Field(fields, "dot3_stats_fcs_errors"),
			Dot3StatsSingleCollisionFrames:     uint32Field(fields, "dot3_stats_single_collision_frames"),
			Dot3StatsMultipleCollisionFrames:   uint32Field(fields, "dot3_stats_multiple_collision_frames"),
			Dot3StatsSQETestErrors:             uint32Field(fields, "dot3_stats_sqe_test_errors"),
			Dot3StatsDeferredTransmissions:     uint32Field(fields, "dot3_stats_deferred_transmissions"),
			Dot3StatsLateCollisions:            uint32Field(fields, "dot3_stats_late_collisions"),
			Dot3StatsExcessiveCollisions:       uint32Field(fields, "dot3_stats_excessive_collisions"),
			Dot3StatsInternalMacTransmitErrors: uint32Field(fields, "dot3_stats_internal_mac_transmit_errors"),
			Dot3StatsCarrierSenseErrors:        uint32Field(fields, "dot3_stats_carrier_sense_errors"),
			Dot3StatsFrameTooLongs:             uint32Field(fields, "dot3_stats_frame_too_longs"),
			Dot3StatsInternalMacReceiveErrors:  uint32Field(fields, "dot3_stats_internal_mac_receive_errors"),
			Dot3StatsSymbolErrors:              uint32Field(fields, "dot3_stats_symbol_errors"),
		}}}
		return append(records, publicRecords...), nil
	default:
		return nil, fmt.Errorf("unsupported sflow counter record_kind %q", stringFieldOrZero(fields, "record_kind"))
	}
}

func preservedSFlowCounterRecords(evt *event.Event) []sflow.CounterRecord {
	if evt == nil || evt.Internal == nil {
		return nil
	}
	records, _ := evt.Internal[sflowCounterRecordsInternalKey].([]sflow.CounterRecord)
	if len(records) == 0 {
		return nil
	}
	out := make([]sflow.CounterRecord, len(records))
	copy(out, records)
	return out
}

func publicSFlowCounterRecords(evt *event.Event) []sflow.CounterRecord {
	if evt == nil || evt.SFlow == nil || len(evt.SFlow.Samples) == 0 {
		return nil
	}
	rawRecords := evt.SFlow.Samples[0].RawCounterRecords
	records := make([]sflow.CounterRecord, 0, len(rawRecords))
	for _, raw := range rawRecords {
		dataFormat, ok := publicSFlowDataFormat(raw.Enterprise, raw.Format, raw.DataFormat)
		if !ok {
			continue
		}
		records = append(records, sflow.CounterRecord{
			Header: sflow.RecordHeader{DataFormat: dataFormat},
			Data:   sflow.RawRecord{Data: append([]byte(nil), raw.Data...)},
		})
	}
	return records
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

// sflowAgentIP resolves the emitted agent IP from event metadata first, then
// the encoder config fallback.
func (e *SFlowEncoder) sflowAgentIP(evt *event.Event) string {
	if agentIP := eventAgentIP(evt); agentIP != "" {
		return agentIP
	}
	if e.cfg.AgentIP != "" {
		return e.cfg.AgentIP
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
	if evt == nil {
		return false
	}
	if len(preservedSFlowCounterRecords(evt)) > 0 {
		return true
	}
	if len(publicSFlowCounterRecords(evt)) > 0 {
		return true
	}
	switch stringFieldOrZero(evt.Fields, "record_kind") {
	case "interface_counter", "ethernet_counter":
		return true
	default:
		return false
	}
}

// truncateLastSampleToFit rewrites only the newest sample when that is enough to
// keep the current packet under the datagram limit.
func (e *SFlowEncoder) truncateLastSampleToFit(packet *sflow.Packet) (sflow.FlowSample, bool, error) {
	lastIdx := len(packet.Samples) - 1
	if lastIdx < 0 {
		return sflow.FlowSample{}, false, nil
	}
	sample, ok := packet.Samples[lastIdx].(sflow.FlowSample)
	if !ok || len(sample.Records) == 0 {
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

// sflowSourceID comes from decoded sFlow metadata or source metadata.
func sflowSourceID(evt *event.Event) uint32 {
	return eventSourceID(evt)
}

// sflowSamplingRate comes from decoded sFlow metadata or source metadata.
func sflowSamplingRate(evt *event.Event) uint32 {
	return eventSamplingRate(evt)
}

// sflowSamplePool comes from decoded sFlow metadata or source metadata.
func sflowSamplePool(evt *event.Event) uint32 {
	return eventSamplePool(evt)
}

// sflowDrops comes from decoded sFlow metadata or source metadata.
func sflowDrops(evt *event.Event) uint32 {
	return eventDrops(evt)
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
