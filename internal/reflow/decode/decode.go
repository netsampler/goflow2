package decode

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

// Decoder identifies and decodes source payloads into the next runtime event shape.
type Decoder interface {
	Decode(evt *event.Event) ([]*event.Event, error)
}

// New returns the built-in decoder used by the current runtime.
func New() Decoder {
	store := templates.NewTemplateFlowStore()
	store.Start()
	return &builtIn{templates: store}
}

type builtIn struct {
	templates *templates.TemplateFlowStore
}

// Decode handles protocol identification for raw flow payloads and passes through other event types.
func (d *builtIn) Decode(evt *event.Event) ([]*event.Event, error) {
	switch evt.Source.Type {
	case "flow":
		return d.decodeFlow(evt)
	case "bytes":
		return decodeBytes(evt)
	default:
		return []*event.Event{evt}, nil
	}
}

func (d *builtIn) decodeFlow(evt *event.Event) ([]*event.Event, error) {
	payload, ok := evt.Payload.([]byte)
	if !ok || len(payload) == 0 {
		return nil, fmt.Errorf("decode flow: missing payload bytes")
	}
	flowType, flowVersion, err := identifyFlow(payload)
	if err != nil {
		return nil, err
	}
	switch flowType {
	case "sflow":
		return d.decodeSFlow(evt, payload, flowVersion)
	case "netflowv5":
		return d.decodeNetFlowV5(evt, payload)
	case "netflowv9":
		return d.decodeNetFlowV9(evt, payload)
	case "ipfix":
		return d.decodeIPFIX(evt, payload)
	default:
		return nil, fmt.Errorf("decode flow: unsupported flow type %q", flowType)
	}
}

func decodeBytes(evt *event.Event) ([]*event.Event, error) {
	payload, ok := evt.Payload.([]byte)
	if !ok || len(payload) == 0 {
		return nil, fmt.Errorf("decode bytes: missing payload bytes")
	}
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, 1)
	}
	evt.Fields["message_type"] = "bytes"
	return []*event.Event{evt}, nil
}

func (d *builtIn) decodeSFlow(evt *event.Event, payload []byte, version uint32) ([]*event.Event, error) {
	packet := &sflow.Packet{}
	if err := sflow.DecodeMessageVersion(bytes.NewBuffer(payload), packet); err != nil {
		return nil, fmt.Errorf("decode sflow: %w", err)
	}

	out := make([]*event.Event, 0, len(packet.Samples))
	for _, sample := range packet.Samples {
		switch s := sample.(type) {
		case sflow.FlowSample:
			out = append(out, d.eventFromSFlowSample(evt, packet, s))
		case *sflow.FlowSample:
			out = append(out, d.eventFromSFlowSample(evt, packet, *s))
		case sflow.ExpandedFlowSample:
			out = append(out, d.eventFromExpandedSFlowSample(evt, packet, s))
		case *sflow.ExpandedFlowSample:
			out = append(out, d.eventFromExpandedSFlowSample(evt, packet, *s))
		}
	}

	if len(out) == 0 {
		base := cloneEvent(evt)
		base.Fields = ensureFields(base, 3)
		base.Fields["message_type"] = "flow"
		base.Fields["flow_type"] = "sflow"
		base.Fields["flow_version"] = version
		return []*event.Event{base}, nil
	}
	return out, nil
}

func (d *builtIn) eventFromSFlowSample(base *event.Event, packet *sflow.Packet, sample sflow.FlowSample) *event.Event {
	evt := cloneEvent(base)
	fields := ensureFields(evt, 16)
	fields["message_type"] = "flow"
	fields["flow_type"] = "sflow"
	fields["flow_version"] = packet.Version
	fields["agent_ip"] = fmt.Sprint(packet.AgentIP)
	fields["sub_agent_id"] = packet.SubAgentId
	fields["source_id"] = sample.Header.SourceIdValue
	fields["sampling_rate"] = sample.SamplingRate
	fields["sample_pool"] = sample.SamplePool
	fields["drops"] = sample.Drops
	fields["input_if"] = sample.Input
	fields["output_if"] = sample.Output
	fields["packets"] = int64(1)

	for _, record := range sample.Records {
		switch data := record.Data.(type) {
		case sflow.SampledHeader:
			fields["protocol"] = data.Protocol
			fields["frame_length"] = data.FrameLength
			fields["stripped"] = data.Stripped
			fields["original_length"] = data.OriginalLength
			fields["header_data"] = data.HeaderData
			fields["bytes"] = int64(data.OriginalLength)
			if tuple, err := parsePacketTuple(data.HeaderData); err == nil {
				fields["src_addr"] = tuple.SrcAddr.String()
				fields["dst_addr"] = tuple.DstAddr.String()
				fields["proto"] = tuple.Proto
				fields["src_port"] = tuple.SrcPort
				fields["dst_port"] = tuple.DstPort
			}
		case sflow.SampledIPv4:
			fields["src_addr"] = fmt.Sprint(data.SrcIP)
			fields["dst_addr"] = fmt.Sprint(data.DstIP)
			fields["src_port"] = data.SrcPort
			fields["dst_port"] = data.DstPort
			fields["proto"] = data.Protocol
			fields["bytes"] = int64(data.Length)
		case sflow.SampledIPv6:
			fields["src_addr"] = fmt.Sprint(data.SrcIP)
			fields["dst_addr"] = fmt.Sprint(data.DstIP)
			fields["src_port"] = data.SrcPort
			fields["dst_port"] = data.DstPort
			fields["proto"] = data.Protocol
			fields["bytes"] = int64(data.Length)
		}
	}

	return evt
}

func (d *builtIn) eventFromExpandedSFlowSample(base *event.Event, packet *sflow.Packet, sample sflow.ExpandedFlowSample) *event.Event {
	evt := cloneEvent(base)
	fields := ensureFields(evt, 16)
	fields["message_type"] = "flow"
	fields["flow_type"] = "sflow"
	fields["flow_version"] = packet.Version
	fields["agent_ip"] = fmt.Sprint(packet.AgentIP)
	fields["sub_agent_id"] = packet.SubAgentId
	fields["source_id"] = sample.Header.SourceIdValue
	fields["sampling_rate"] = sample.SamplingRate
	fields["sample_pool"] = sample.SamplePool
	fields["drops"] = sample.Drops
	fields["input_if"] = sample.InputIfValue
	fields["output_if"] = sample.OutputIfValue
	fields["packets"] = int64(1)

	for _, record := range sample.Records {
		if data, ok := record.Data.(sflow.SampledHeader); ok {
			fields["protocol"] = data.Protocol
			fields["frame_length"] = data.FrameLength
			fields["stripped"] = data.Stripped
			fields["original_length"] = data.OriginalLength
			fields["header_data"] = data.HeaderData
			fields["bytes"] = int64(data.OriginalLength)
			if tuple, err := parsePacketTuple(data.HeaderData); err == nil {
				fields["src_addr"] = tuple.SrcAddr.String()
				fields["dst_addr"] = tuple.DstAddr.String()
				fields["proto"] = tuple.Proto
				fields["src_port"] = tuple.SrcPort
				fields["dst_port"] = tuple.DstPort
			}
		}
	}

	return evt
}

func (d *builtIn) decodeNetFlowV5(evt *event.Event, payload []byte) ([]*event.Event, error) {
	packet := &netflowlegacy.PacketNetFlowV5{}
	if err := netflowlegacy.DecodeMessageVersion(bytes.NewBuffer(payload), packet); err != nil {
		return nil, fmt.Errorf("decode netflow v5: %w", err)
	}

	out := make([]*event.Event, 0, len(packet.Records))
	for _, record := range packet.Records {
		item := cloneEvent(evt)
		fields := ensureFields(item, 16)
		fields["message_type"] = "flow"
		fields["flow_type"] = "netflowv5"
		fields["flow_version"] = packet.Version
		fields["src_addr"] = fmt.Sprint(record.SrcAddr)
		fields["dst_addr"] = fmt.Sprint(record.DstAddr)
		fields["src_port"] = uint32(record.SrcPort)
		fields["dst_port"] = uint32(record.DstPort)
		fields["proto"] = uint32(record.Proto)
		fields["bytes"] = int64(record.DOctets)
		fields["packets"] = int64(record.DPkts)
		fields["input_if"] = uint32(record.Input)
		fields["output_if"] = uint32(record.Output)
		fields["start_time_unix"] = flowTimeFromV5(packet.UnixSecs, packet.UnixNSecs, packet.SysUptime, record.First)
		fields["end_time_unix"] = flowTimeFromV5(packet.UnixSecs, packet.UnixNSecs, packet.SysUptime, record.Last)
		out = append(out, item)
	}
	return out, nil
}

func (d *builtIn) decodeNetFlowV9(evt *event.Event, payload []byte) ([]*event.Event, error) {
	packet := &netflow.NFv9Packet{}
	ctx := netflow.FlowContext{RouterKey: routerKey(evt)}
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), d.templates, ctx, packet, nil); err != nil {
		return nil, fmt.Errorf("decode netflow v9: %w", err)
	}

	var out []*event.Event
	for _, flowSet := range packet.FlowSets {
		dataSet, ok := flowSet.(netflow.DataFlowSet)
		if !ok {
			continue
		}
		for _, record := range dataSet.Records {
			item := cloneEvent(evt)
			fields := ensureFields(item, 16)
			fields["message_type"] = "flow"
			fields["flow_type"] = "netflowv9"
			fields["flow_version"] = packet.Version
			mapDataFields(fields, record.Values, 9, packet.SystemUptime, packet.UnixSeconds)
			out = append(out, item)
		}
	}
	return out, nil
}

func (d *builtIn) decodeIPFIX(evt *event.Event, payload []byte) ([]*event.Event, error) {
	packet := &netflow.IPFIXPacket{}
	ctx := netflow.FlowContext{RouterKey: routerKey(evt)}
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), d.templates, ctx, nil, packet); err != nil {
		return nil, fmt.Errorf("decode ipfix: %w", err)
	}

	var out []*event.Event
	for _, flowSet := range packet.FlowSets {
		dataSet, ok := flowSet.(netflow.DataFlowSet)
		if !ok {
			continue
		}
		for _, record := range dataSet.Records {
			item := cloneEvent(evt)
			fields := ensureFields(item, 16)
			fields["message_type"] = "flow"
			fields["flow_type"] = "ipfix"
			fields["flow_version"] = packet.Version
			mapDataFields(fields, record.Values, 10, 0, 0)
			out = append(out, item)
		}
	}
	return out, nil
}

func mapDataFields(fields map[string]any, values []netflow.DataField, version uint16, sysUptime, unixSeconds uint32) {
	for _, field := range values {
		switch field.Type {
		case 4:
			fields["proto"] = decodeUint32(field.Value)
		case 7:
			fields["src_port"] = decodeUint32(field.Value)
		case 11:
			fields["dst_port"] = decodeUint32(field.Value)
		case 8, 27:
			fields["src_addr"] = decodeIPString(field.Value)
		case 12, 28:
			fields["dst_addr"] = decodeIPString(field.Value)
		case 1:
			fields["bytes"] = int64(decodeUint64(field.Value))
		case 2:
			fields["packets"] = int64(decodeUint64(field.Value))
		case 10:
			fields["input_if"] = decodeUint32(field.Value)
		case 14:
			fields["output_if"] = decodeUint32(field.Value)
		case 34:
			fields["sampling_rate"] = decodeUint32(field.Value)
		case netflow.NFV9_FIELD_FIRST_SWITCHED:
			fields["start_time_unix"] = flowTimeFromV9(sysUptime, unixSeconds, decodeUint32(field.Value))
		case netflow.NFV9_FIELD_LAST_SWITCHED:
			fields["end_time_unix"] = flowTimeFromV9(sysUptime, unixSeconds, decodeUint32(field.Value))
		case netflow.IPFIX_FIELD_flowStartMilliseconds:
			fields["start_time_unix"] = int64(decodeUint64(field.Value))
		case netflow.IPFIX_FIELD_flowEndMilliseconds:
			fields["end_time_unix"] = int64(decodeUint64(field.Value))
		}
	}
}

func identifyFlow(payload []byte) (string, uint32, error) {
	if len(payload) < 4 {
		return "", 0, fmt.Errorf("identify flow: payload too short")
	}
	if binary.BigEndian.Uint32(payload[:4]) == 5 {
		return "sflow", 5, nil
	}
	switch version := binary.BigEndian.Uint16(payload[:2]); version {
	case 5:
		return "netflowv5", uint32(version), nil
	case 9:
		return "netflowv9", uint32(version), nil
	case 10:
		return "ipfix", uint32(version), nil
	default:
		return "", 0, fmt.Errorf("identify flow: unsupported version %d", version)
	}
}

func routerKey(evt *event.Event) string {
	if evt.Source.Remote != "" {
		return evt.Source.Remote
	}
	return evt.Source.Address
}

func cloneEvent(evt *event.Event) *event.Event {
	item := &event.Event{
		ReceivedAt: evt.ReceivedAt,
		Source:     evt.Source,
		Message:    evt.Message,
		Payload:    evt.Payload,
	}
	if evt.Fields != nil {
		item.Fields = make(map[string]any, len(evt.Fields))
		for k, v := range evt.Fields {
			item.Fields[k] = v
		}
	}
	return item
}

func ensureFields(evt *event.Event, capacity int) map[string]any {
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, capacity)
	}
	return evt.Fields
}

func decodeUint32(val any) uint32 {
	switch v := val.(type) {
	case []byte:
		return uint32(decodeUint64(v))
	case uint32:
		return v
	case uint64:
		return uint32(v)
	default:
		return 0
	}
}

func decodeUint64(val any) uint64 {
	switch v := val.(type) {
	case []byte:
		switch len(v) {
		case 1:
			return uint64(v[0])
		case 2:
			return uint64(binary.BigEndian.Uint16(v))
		case 4:
			return uint64(binary.BigEndian.Uint32(v))
		case 8:
			return binary.BigEndian.Uint64(v)
		default:
			var out uint64
			for _, b := range v {
				out = (out << 8) | uint64(b)
			}
			return out
		}
	case uint64:
		return v
	case uint32:
		return uint64(v)
	default:
		return 0
	}
}

func decodeIPString(val any) string {
	raw, ok := val.([]byte)
	if !ok {
		return fmt.Sprint(val)
	}
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return ""
	}
	return addr.String()
}

func flowTimeFromV5(unixSecs, unixNSecs, sysUptime, switched uint32) int64 {
	exportMs := int64(unixSecs)*1000 + int64(unixNSecs)/1_000_000
	uptimeMs := int64(sysUptime)
	return exportMs - (uptimeMs - int64(switched))
}

func flowTimeFromV9(sysUptime, unixSeconds, switched uint32) int64 {
	exportMs := int64(unixSeconds) * 1000
	uptimeMs := int64(sysUptime)
	return exportMs - (uptimeMs - int64(switched))
}

type packetTuple struct {
	SrcAddr netip.Addr
	DstAddr netip.Addr
	Proto   uint32
	SrcPort uint32
	DstPort uint32
}

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
