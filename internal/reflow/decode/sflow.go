package decode

import (
	"bytes"
	"fmt"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

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
		case sflow.CounterSample:
			out = append(out, d.eventsFromSFlowCounterSample(evt, packet, s)...)
		case *sflow.CounterSample:
			out = append(out, d.eventsFromSFlowCounterSample(evt, packet, *s)...)
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

// eventFromSFlowSample converts one sFlow flow sample into the runtime's
// canonical field map while retaining original sFlow metadata.
func (d *builtIn) eventFromSFlowSample(base *event.Event, packet *sflow.Packet, sample sflow.FlowSample) *event.Event {
	evt := cloneEvent(base)
	fields := ensureFields(evt, 16)
	agentIP := ipSliceString(packet.AgentIP)
	evt.SFlow = &event.SFlowMetadata{
		AgentIP:        agentIP,
		SubAgentID:     packet.SubAgentId,
		SequenceNumber: packet.SequenceNumber,
		Uptime:         packet.Uptime,
		SourceID:       sample.Header.SourceIdValue,
		SamplingRate:   sample.SamplingRate,
		SamplePool:     sample.SamplePool,
		Drops:          sample.Drops,
	}
	fields["message_type"] = "flow"
	fields["flow_type"] = "sflow"
	fields["flow_version"] = packet.Version
	fields["agent_ip"] = agentIP
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
			fields["record_kind"] = "packet"
			// Keep the raw sampled header as bytes so downstream packet handling can
			// treat sFlow packet samples and source.type=bytes events consistently.
			fields["header_data"] = append([]byte(nil), data.HeaderData...)
			fields["bytes"] = int64(data.OriginalLength)
		case sflow.SampledIPv4:
			fields["src_addr"] = fmt.Sprint(data.SrcIP)
			fields["dst_addr"] = fmt.Sprint(data.DstIP)
			fields["src_port"] = data.SrcPort
			fields["dst_port"] = data.DstPort
			fields["proto"] = data.Protocol
			fields["proto_name"] = ipProtocolName(uint32(data.Protocol))
			fields["bytes"] = int64(data.Length)
		case sflow.SampledIPv6:
			fields["src_addr"] = fmt.Sprint(data.SrcIP)
			fields["dst_addr"] = fmt.Sprint(data.DstIP)
			fields["src_port"] = data.SrcPort
			fields["dst_port"] = data.DstPort
			fields["proto"] = data.Protocol
			fields["proto_name"] = ipProtocolName(uint32(data.Protocol))
			fields["bytes"] = int64(data.Length)
		}
	}

	return evt
}

// eventFromExpandedSFlowSample handles the expanded sFlow format, which carries
// the same logical data with explicit interface encodings.
func (d *builtIn) eventFromExpandedSFlowSample(base *event.Event, packet *sflow.Packet, sample sflow.ExpandedFlowSample) *event.Event {
	evt := cloneEvent(base)
	fields := ensureFields(evt, 16)
	agentIP := ipSliceString(packet.AgentIP)
	evt.SFlow = &event.SFlowMetadata{
		AgentIP:        agentIP,
		SubAgentID:     packet.SubAgentId,
		SequenceNumber: packet.SequenceNumber,
		Uptime:         packet.Uptime,
		SourceID:       sample.Header.SourceIdValue,
		SamplingRate:   sample.SamplingRate,
		SamplePool:     sample.SamplePool,
		Drops:          sample.Drops,
	}
	fields["message_type"] = "flow"
	fields["flow_type"] = "sflow"
	fields["flow_version"] = packet.Version
	fields["agent_ip"] = agentIP
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
			fields["record_kind"] = "packet"
			fields["header_data"] = append([]byte(nil), data.HeaderData...)
			fields["bytes"] = int64(data.OriginalLength)
		}
	}

	return evt
}

// eventsFromSFlowCounterSample emits one event per interface counter record so
// later stages can aggregate or re-encode them independently.
func (d *builtIn) eventsFromSFlowCounterSample(base *event.Event, packet *sflow.Packet, sample sflow.CounterSample) []*event.Event {
	agentIP := ipSliceString(packet.AgentIP)
	out := make([]*event.Event, 0, len(sample.Records))
	for _, record := range sample.Records {
		data, ok := record.Data.(sflow.IfCounters)
		if !ok {
			continue
		}
		evt := cloneEvent(base)
		fields := ensureFields(evt, 24)
		evt.SFlow = &event.SFlowMetadata{
			AgentIP:        agentIP,
			SubAgentID:     packet.SubAgentId,
			SequenceNumber: packet.SequenceNumber,
			Uptime:         packet.Uptime,
			SourceID:       sample.Header.SourceIdValue,
		}
		fields["message_type"] = "counter"
		fields["counter_type"] = "sflow"
		fields["record_kind"] = "interface_counter"
		fields["sflow_version"] = packet.Version
		fields["agent_ip"] = agentIP
		fields["sub_agent_id"] = packet.SubAgentId
		fields["source_id"] = sample.Header.SourceIdValue
		fields["if_index"] = data.IfIndex
		fields["if_type"] = data.IfType
		fields["if_speed"] = data.IfSpeed
		fields["if_direction"] = data.IfDirection
		fields["if_status"] = data.IfStatus
		fields["if_in_octets"] = data.IfInOctets
		fields["if_in_ucast_pkts"] = data.IfInUcastPkts
		fields["if_in_multicast_pkts"] = data.IfInMulticastPkts
		fields["if_in_broadcast_pkts"] = data.IfInBroadcastPkts
		fields["if_in_discards"] = data.IfInDiscards
		fields["if_in_errors"] = data.IfInErrors
		fields["if_in_unknown_protos"] = data.IfInUnknownProtos
		fields["if_out_octets"] = data.IfOutOctets
		fields["if_out_ucast_pkts"] = data.IfOutUcastPkts
		fields["if_out_multicast_pkts"] = data.IfOutMulticastPkts
		fields["if_out_broadcast_pkts"] = data.IfOutBroadcastPkts
		fields["if_out_discards"] = data.IfOutDiscards
		fields["if_out_errors"] = data.IfOutErrors
		fields["if_promiscuous_mode"] = data.IfPromiscuousMode
		out = append(out, evt)
	}
	return out
}
