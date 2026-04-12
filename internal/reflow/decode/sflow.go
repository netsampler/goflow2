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
