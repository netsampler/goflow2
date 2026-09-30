package decode

import (
	"bytes"
	"fmt"
	"net/netip"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

const sflowFlowRecordsInternalKey = "sflow_flow_records"
const sflowSampleInternalKey = "sflow_sample"
const sflowCounterRecordsInternalKey = "sflow_counter_records"

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
		case sflow.RawSample:
			out = append(out, d.eventFromRawSFlowSample(evt, packet, s))
		case *sflow.RawSample:
			out = append(out, d.eventFromRawSFlowSample(evt, packet, *s))
		}
	}

	if len(out) == 0 {
		base := cloneEvent(evt)
		base.Fields = ensureFields(base, 3)
		base.Fields["flow_type"] = "sflow"
		base.Fields["flow_version"] = version
		return []*event.Event{base}, nil
	}
	for _, item := range out {
		ensureSFlowDecodedFields(item, packet.Version)
	}
	return out, nil
}

func ensureSFlowDecodedFields(evt *event.Event, version uint32) {
	fields := ensureFields(evt, 2)
	if _, ok := fields["flow_type"]; !ok {
		fields["flow_type"] = "sflow"
	}
	if _, ok := fields["flow_version"]; !ok {
		fields["flow_version"] = version
	}
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
		if shouldPreserveSFlowFlowRecord(record) {
			preserveSFlowFlowRecord(evt, record)
		}
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
			fields["bytes"] = int64(data.FrameLength)
		case sflow.SampledIPv4:
			fields["src_addr"] = string(data.SrcIP)
			fields["dst_addr"] = string(data.DstIP)
			fields["src_port"] = data.SrcPort
			fields["dst_port"] = data.DstPort
			fields["proto"] = data.Protocol
			fields["proto_name"] = ipProtocolName(uint32(data.Protocol))
			fields["bytes"] = int64(data.Length)
		case sflow.SampledIPv6:
			fields["src_addr"] = string(data.SrcIP)
			fields["dst_addr"] = string(data.DstIP)
			fields["src_port"] = data.SrcPort
			fields["dst_port"] = data.DstPort
			fields["proto"] = data.Protocol
			fields["proto_name"] = ipProtocolName(uint32(data.Protocol))
			fields["bytes"] = int64(data.Length)
		case sflow.ExtendedNAT:
			applySFlowExtendedNATFields(fields, data)
		case sflow.ExtendedMPLS:
			applySFlowExtendedMPLSFields(fields, data)
		case sflow.ExtendedMPLSTunnel:
			fields["mpls_tunnel_lsp_name"] = data.TunnelLSPName
			fields["mpls_tunnel_id"] = data.TunnelID
			fields["mpls_tunnel_cos"] = data.TunnelCOS
		case sflow.ExtendedMPLSVC:
			fields["mpls_vc_instance_name"] = data.VCInstanceName
			fields["mpls_vll_vc_id"] = data.VLLVCID
			fields["mpls_vc_label_cos"] = data.VCLabelCOS
		case sflow.ExtendedMPLSFTN:
			fields["mpls_ftn_descr"] = data.MPLSFTNDescr
			fields["mpls_ftn_mask"] = data.MPLSFTNMask
		case sflow.ExtendedMPLSLDPFEC:
			fields["mpls_fec_addr_prefix_length"] = data.MPLSFecAddrPrefixLength
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
		if shouldPreserveSFlowFlowRecord(record) {
			preserveSFlowFlowRecord(evt, record)
		}
		if data, ok := record.Data.(sflow.SampledHeader); ok {
			fields["protocol"] = data.Protocol
			fields["frame_length"] = data.FrameLength
			fields["stripped"] = data.Stripped
			fields["original_length"] = data.OriginalLength
			fields["record_kind"] = "packet"
			fields["header_data"] = append([]byte(nil), data.HeaderData...)
			fields["bytes"] = int64(data.FrameLength)
			continue
		}
		applySFlowExtendedRecordFields(fields, record.Data)
	}

	return evt
}

func (d *builtIn) eventFromRawSFlowSample(base *event.Event, packet *sflow.Packet, sample sflow.RawSample) *event.Event {
	evt := cloneEvent(base)
	fields := ensureFields(evt, 8)
	agentIP := ipSliceString(packet.AgentIP)
	evt.SFlow = &event.SFlowMetadata{
		AgentIP:        agentIP,
		SubAgentID:     packet.SubAgentId,
		SequenceNumber: packet.SequenceNumber,
		Uptime:         packet.Uptime,
	}
	fields["flow_type"] = "sflow"
	fields["flow_version"] = packet.Version
	fields["agent_ip"] = agentIP
	fields["sub_agent_id"] = packet.SubAgentId
	fields["record_kind"] = "sflow_raw_sample"
	if evt.Internal == nil {
		evt.Internal = make(map[string]any, 1)
	}
	evt.Internal[sflowSampleInternalKey] = sample
	return evt
}

// eventsFromSFlowCounterSample emits one event per interface counter record so
// later stages can aggregate or re-encode them independently.
func (d *builtIn) eventsFromSFlowCounterSample(base *event.Event, packet *sflow.Packet, sample sflow.CounterSample) []*event.Event {
	agentIP := ipSliceString(packet.AgentIP)
	out := make([]*event.Event, 0, len(sample.Records))
	for _, record := range sample.Records {
		evt := cloneEvent(base)
		fields := ensureFields(evt, 24)
		evt.SFlow = &event.SFlowMetadata{
			AgentIP:        agentIP,
			SubAgentID:     packet.SubAgentId,
			SequenceNumber: packet.SequenceNumber,
			Uptime:         packet.Uptime,
			SourceID:       sample.Header.SourceIdValue,
		}
		fields["flow_type"] = "sflow"
		fields["flow_version"] = packet.Version
		fields["counter_type"] = "sflow"
		fields["sflow_version"] = packet.Version
		fields["agent_ip"] = agentIP
		fields["sub_agent_id"] = packet.SubAgentId
		fields["source_id_type"] = sample.Header.SourceIdType
		fields["source_id"] = sample.Header.SourceIdValue
		fields["counter_record_format"] = record.Header.DataFormat
		if sample.Header.Format == sflow.SAMPLE_FORMAT_EXPANDED_COUNTER {
			fields["counter_format"] = "expanded"
		}
		applySFlowCounterRecordFields(evt, fields, record)
		out = append(out, evt)
	}
	return out
}

func preserveSFlowCounterRecord(evt *event.Event, record sflow.CounterRecord) {
	if evt.Internal == nil {
		evt.Internal = make(map[string]any, 1)
	}
	records, _ := evt.Internal[sflowCounterRecordsInternalKey].([]sflow.CounterRecord)
	evt.Internal[sflowCounterRecordsInternalKey] = append(records, record)
}

func applySFlowCounterRecordFields(evt *event.Event, fields map[string]any, record sflow.CounterRecord) {
	switch data := record.Data.(type) {
	case sflow.IfCounters:
		fields["record_kind"] = "interface_counter"
		applySFlowIfCounterFields(fields, data)
	case sflow.EthernetCounters:
		fields["record_kind"] = "ethernet_counter"
		applySFlowEthernetCounterFields(fields, data)
	default:
		fields["record_kind"] = "counter_record"
		preserveSFlowCounterRecord(evt, record)
	}
}

func applySFlowIfCounterFields(fields map[string]any, data sflow.IfCounters) {
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
}

func applySFlowEthernetCounterFields(fields map[string]any, data sflow.EthernetCounters) {
	fields["dot3_stats_alignment_errors"] = data.Dot3StatsAlignmentErrors
	fields["dot3_stats_fcs_errors"] = data.Dot3StatsFCSErrors
	fields["dot3_stats_single_collision_frames"] = data.Dot3StatsSingleCollisionFrames
	fields["dot3_stats_multiple_collision_frames"] = data.Dot3StatsMultipleCollisionFrames
	fields["dot3_stats_sqe_test_errors"] = data.Dot3StatsSQETestErrors
	fields["dot3_stats_deferred_transmissions"] = data.Dot3StatsDeferredTransmissions
	fields["dot3_stats_late_collisions"] = data.Dot3StatsLateCollisions
	fields["dot3_stats_excessive_collisions"] = data.Dot3StatsExcessiveCollisions
	fields["dot3_stats_internal_mac_transmit_errors"] = data.Dot3StatsInternalMacTransmitErrors
	fields["dot3_stats_carrier_sense_errors"] = data.Dot3StatsCarrierSenseErrors
	fields["dot3_stats_frame_too_longs"] = data.Dot3StatsFrameTooLongs
	fields["dot3_stats_internal_mac_receive_errors"] = data.Dot3StatsInternalMacReceiveErrors
	fields["dot3_stats_symbol_errors"] = data.Dot3StatsSymbolErrors
}

func shouldPreserveSFlowFlowRecord(record sflow.FlowRecord) bool {
	switch record.Data.(type) {
	case sflow.SampledHeader, sflow.SampledEthernet, sflow.SampledIPv4, sflow.SampledIPv6:
		return false
	default:
		return true
	}
}

func preserveSFlowFlowRecord(evt *event.Event, record sflow.FlowRecord) {
	if evt.Internal == nil {
		evt.Internal = make(map[string]any, 1)
	}
	records, _ := evt.Internal[sflowFlowRecordsInternalKey].([]sflow.FlowRecord)
	evt.Internal[sflowFlowRecordsInternalKey] = append(records, record)
}

func applySFlowExtendedRecordFields(fields map[string]any, data any) {
	switch data := data.(type) {
	case sflow.ExtendedNAT:
		applySFlowExtendedNATFields(fields, data)
	case sflow.ExtendedMPLS:
		applySFlowExtendedMPLSFields(fields, data)
	case sflow.ExtendedMPLSTunnel:
		fields["mpls_tunnel_lsp_name"] = data.TunnelLSPName
		fields["mpls_tunnel_id"] = data.TunnelID
		fields["mpls_tunnel_cos"] = data.TunnelCOS
	case sflow.ExtendedMPLSVC:
		fields["mpls_vc_instance_name"] = data.VCInstanceName
		fields["mpls_vll_vc_id"] = data.VLLVCID
		fields["mpls_vc_label_cos"] = data.VCLabelCOS
	case sflow.ExtendedMPLSFTN:
		fields["mpls_ftn_descr"] = data.MPLSFTNDescr
		fields["mpls_ftn_mask"] = data.MPLSFTNMask
	case sflow.ExtendedMPLSLDPFEC:
		fields["mpls_fec_addr_prefix_length"] = data.MPLSFecAddrPrefixLength
	}
}

func applySFlowExtendedNATFields(fields map[string]any, data sflow.ExtendedNAT) {
	if src := sflowIPString(data.SrcAddress); src != "" {
		fields["nat_src_addr"] = src
	}
	if dst := sflowIPString(data.DstAddress); dst != "" {
		fields["nat_dst_addr"] = dst
	}
}

func applySFlowExtendedMPLSFields(fields map[string]any, data sflow.ExtendedMPLS) {
	if nextHop := sflowIPString(data.NextHop); nextHop != "" {
		fields["mpls_next_hop_addr"] = nextHop
	}
	fields["mpls_in_label_stack"] = append([]uint32(nil), data.InLabelStack...)
	fields["mpls_out_label_stack"] = append([]uint32(nil), data.OutLabelStack...)
	for i, entry := range data.InLabelStack {
		index := i + 1
		fields[fmt.Sprintf("mpls_label_%d", index)] = mplsLabelValueFromStackEntry(entry)
		fields[fmt.Sprintf("mpls_label_stack_section_%d", index)] = mplsLabelStackSectionFromEntry(entry)
	}
}

func sflowIPString(ip []byte) string {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return ""
	}
	return addr.String()
}

func mplsLabelValueFromStackEntry(entry uint32) uint32 {
	return (entry >> 12) & 0xfffff
}

func mplsLabelStackSectionFromEntry(entry uint32) []byte {
	return []byte{byte(entry >> 24), byte(entry >> 16), byte(entry >> 8)}
}
