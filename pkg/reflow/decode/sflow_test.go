package decode

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func TestDecodeSFlowCounterSampleEmitsInterfaceCounterEvent(t *testing.T) {
	packet := &sflow.Packet{
		Version:        5,
		AgentIP:        utils.IPAddress{198, 51, 100, 1},
		SubAgentId:     7,
		SequenceNumber: 8,
		Uptime:         9,
		Samples: []interface{}{
			sflow.CounterSample{
				Header: sflow.SampleHeader{
					Format:               sflow.SAMPLE_FORMAT_COUNTER,
					SampleSequenceNumber: 10,
					SourceIdType:         0,
					SourceIdValue:        11,
				},
				CounterRecordsCount: 1,
				Records: []sflow.CounterRecord{
					{
						Data: sflow.IfCounters{
							IfIndex:       12,
							IfSpeed:       1000,
							IfInOctets:    123,
							IfOutOctets:   456,
							IfOutErrors:   3,
							IfStatus:      5,
							IfDirection:   1,
							IfInDiscards:  2,
							IfOutDiscards: 4,
						},
					},
				},
			},
		},
	}

	encoded, err := sflow.EncodeMessage(packet)
	if err != nil {
		t.Fatalf("EncodeMessage returned error: %v", err)
	}

	decoded, err := (&builtIn{}).decodeSFlow(&event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Payload: append([]byte(nil), encoded...),
	}, encoded, 5)
	if err != nil {
		t.Fatalf("decodeSFlow returned error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 decoded event, got %d", len(decoded))
	}

	fields := decoded[0].Fields
	if got := fields["flow_type"]; got != "sflow" {
		t.Fatalf("expected flow_type=sflow, got %#v", got)
	}
	if got := fields["flow_version"]; got != uint32(5) {
		t.Fatalf("expected flow_version=5, got %#v", got)
	}
	if got := fields["record_kind"]; got != "interface_counter" {
		t.Fatalf("expected record_kind=interface_counter, got %#v", got)
	}
	if got := fields["agent_ip"]; got != "198.51.100.1" {
		t.Fatalf("expected agent_ip=198.51.100.1, got %#v", got)
	}
	if got := fields["if_index"]; got != uint32(12) {
		t.Fatalf("expected if_index=12, got %#v", got)
	}
	if got := fields["if_in_octets"]; got != uint64(123) {
		t.Fatalf("expected if_in_octets=123, got %#v", got)
	}
	if got := fields["if_out_octets"]; got != uint64(456) {
		t.Fatalf("expected if_out_octets=456, got %#v", got)
	}

	if decoded[0].SFlow == nil || decoded[0].SFlow.SourceID != 11 {
		t.Fatalf("expected sflow metadata source_id=11, got %#v", decoded[0].SFlow)
	}

	roundTrip := &sflow.Packet{}
	if err := sflow.DecodeMessageVersion(bytes.NewBuffer(encoded), roundTrip); err != nil {
		t.Fatalf("DecodeMessageVersion sanity check failed: %v", err)
	}
}

func TestDecodeSFlowExpandedCounterSampleCarriesFlowAndCounterFormat(t *testing.T) {
	packet := &sflow.Packet{
		Version:        5,
		AgentIP:        utils.IPAddress{198, 51, 100, 1},
		SubAgentId:     7,
		SequenceNumber: 8,
		Uptime:         9,
		Samples: []interface{}{
			sflow.CounterSample{
				Header: sflow.SampleHeader{
					Format:               sflow.SAMPLE_FORMAT_EXPANDED_COUNTER,
					SampleSequenceNumber: 10,
					SourceIdType:         2,
					SourceIdValue:        11,
				},
				CounterRecordsCount: 1,
				Records: []sflow.CounterRecord{
					{
						Data: sflow.IfCounters{
							IfIndex:     12,
							IfInOctets:  123,
							IfOutOctets: 456,
						},
					},
				},
			},
		},
	}

	encoded, err := sflow.EncodeMessage(packet)
	if err != nil {
		t.Fatalf("EncodeMessage returned error: %v", err)
	}

	decoded, err := (&builtIn{}).decodeSFlow(&event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Payload: append([]byte(nil), encoded...),
	}, encoded, 5)
	if err != nil {
		t.Fatalf("decodeSFlow returned error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 decoded event, got %d", len(decoded))
	}

	fields := decoded[0].Fields
	if got := fields["flow_type"]; got != "sflow" {
		t.Fatalf("expected flow_type=sflow, got %#v", got)
	}
	if got := fields["counter_format"]; got != "expanded" {
		t.Fatalf("expected counter_format=expanded, got %#v", got)
	}
	if got := fields["source_id_type"]; got != uint32(2) {
		t.Fatalf("expected source_id_type=2, got %#v", got)
	}
}

func TestDecodeSFlowEthernetCounterSampleEmitsCounterEvent(t *testing.T) {
	packet := &sflow.Packet{
		Version:        5,
		AgentIP:        utils.IPAddress{198, 51, 100, 1},
		SubAgentId:     7,
		SequenceNumber: 8,
		Uptime:         9,
		Samples: []interface{}{
			sflow.CounterSample{
				Header: sflow.SampleHeader{
					Format:               sflow.SAMPLE_FORMAT_COUNTER,
					SampleSequenceNumber: 10,
					SourceIdValue:        11,
				},
				CounterRecordsCount: 1,
				Records: []sflow.CounterRecord{
					{
						Data: sflow.EthernetCounters{
							Dot3StatsFCSErrors:     2,
							Dot3StatsSymbolErrors:  13,
							Dot3StatsFrameTooLongs: 11,
						},
					},
				},
			},
		},
	}

	encoded, err := sflow.EncodeMessage(packet)
	if err != nil {
		t.Fatalf("EncodeMessage returned error: %v", err)
	}

	decoded, err := (&builtIn{}).decodeSFlow(&event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Payload: append([]byte(nil), encoded...),
	}, encoded, 5)
	if err != nil {
		t.Fatalf("decodeSFlow returned error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 decoded event, got %d", len(decoded))
	}

	fields := decoded[0].Fields
	if got := fields["flow_type"]; got != "sflow" {
		t.Fatalf("expected flow_type=sflow, got %#v", got)
	}
	if got := fields["record_kind"]; got != "ethernet_counter" {
		t.Fatalf("expected record_kind=ethernet_counter, got %#v", got)
	}
	if got := fields["dot3_stats_fcs_errors"]; got != uint32(2) {
		t.Fatalf("expected dot3_stats_fcs_errors=2, got %#v", got)
	}
	if got := fields["dot3_stats_symbol_errors"]; got != uint32(13) {
		t.Fatalf("expected dot3_stats_symbol_errors=13, got %#v", got)
	}
}

func TestDecodeSFlowCounterPacketExample(t *testing.T) {
	payload, err := hex.DecodeString("0000000500000001c0a800ac00000000000000290010b171000000040000000400000070000000a10000000000000000000000010000000100000058000000110000000600000000000000000000000000000003000000000ae66a130002336f00000000000000000000000000000000000000000000000d786230000bee300000000000000000000000000000000000000020000000400000070000000a200000000000000000000000100000001000000580000000f0000000600000000000000000000000000000003000000083726775302207a1a000000000000000000000000000000000000000002293935c100e65a01000000000000000000000000000000000000000200000004000000700000000a3000000000000000000000001000000010000005800000010000000060000000000000000000000000000000300000009ab59d28c020ae0cb00000000000000000000000000000000000000000105914cc500b29fff00000000000000000000000000000000000000020000000400000070000000a4000000000000000000000001000000010000005800000012000000060000000000000000000000000000000300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000002")
	if err != nil {
		t.Fatalf("DecodeString returned error: %v", err)
	}

	decoded, err := (&builtIn{}).decodeSFlow(&event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Payload: append([]byte(nil), payload...),
	}, payload, 5)
	if err != nil {
		t.Fatalf("decodeSFlow returned error: %v", err)
	}
	if len(decoded) != 4 {
		t.Fatalf("expected 4 decoded events, got %d", len(decoded))
	}
	for i, evt := range decoded {
		if got := evt.Fields["flow_type"]; got != "sflow" {
			t.Fatalf("event %d expected flow_type=sflow, got %#v fields=%#v", i, got, evt.Fields)
		}
		if got := evt.Fields["record_kind"]; got != "interface_counter" {
			t.Fatalf("event %d expected record_kind=interface_counter, got %#v fields=%#v", i, got, evt.Fields)
		}
	}
}

func TestDecodeSFlowSampledHeaderBytesUseFrameLength(t *testing.T) {
	packet := &sflow.Packet{
		Version:        5,
		AgentIP:        utils.IPAddress{198, 51, 100, 1},
		SubAgentId:     7,
		SequenceNumber: 8,
		Uptime:         9,
		Samples: []interface{}{
			sflow.FlowSample{
				Header: sflow.SampleHeader{
					Format:               sflow.SAMPLE_FORMAT_FLOW,
					SampleSequenceNumber: 10,
					SourceIdType:         0,
					SourceIdValue:        11,
				},
				SamplingRate:     100,
				SamplePool:       1000,
				FlowRecordsCount: 1,
				Records: []sflow.FlowRecord{
					{
						Data: sflow.SampledHeader{
							Protocol:       1,
							FrameLength:    74,
							OriginalLength: 54,
							HeaderData:     bytes.Repeat([]byte{0xaa}, 54),
						},
					},
				},
			},
		},
	}

	encoded, err := sflow.EncodeMessage(packet)
	if err != nil {
		t.Fatalf("EncodeMessage returned error: %v", err)
	}

	decoded, err := (&builtIn{}).decodeSFlow(&event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Payload: append([]byte(nil), encoded...),
	}, encoded, 5)
	if err != nil {
		t.Fatalf("decodeSFlow returned error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 decoded event, got %d", len(decoded))
	}
	if got := decoded[0].Fields["bytes"]; got != int64(74) {
		t.Fatalf("expected bytes to use frame_length=74, got %#v", got)
	}
}

func TestDecodeSFlowExtendedNATMPLSFieldsAndPreservesRecords(t *testing.T) {
	packet := &sflow.Packet{
		Version:        5,
		AgentIP:        utils.IPAddress{198, 51, 100, 1},
		SubAgentId:     7,
		SequenceNumber: 8,
		Uptime:         9,
		Samples: []interface{}{
			sflow.FlowSample{
				Header: sflow.SampleHeader{
					Format:               sflow.SAMPLE_FORMAT_FLOW,
					SampleSequenceNumber: 10,
					SourceIdValue:        11,
				},
				Records: []sflow.FlowRecord{
					{Data: sflow.SampledHeader{Protocol: 1, FrameLength: 64, OriginalLength: 4, HeaderData: []byte{1, 2, 3, 4}}},
					{Data: sflow.ExtendedNAT{SrcAddress: utils.IPAddress{203, 0, 113, 10}, DstAddress: utils.IPAddress{192, 0, 2, 20}}},
					{Data: sflow.ExtendedMPLS{NextHop: utils.IPAddress{192, 0, 2, 254}, InLabelStack: []uint32{0x00011100}, OutLabelStack: []uint32{0x00022200}}},
					{Data: sflow.ExtendedMPLSTunnel{TunnelLSPName: "lsp-a", TunnelID: 12, TunnelCOS: 3}},
				},
			},
		},
	}

	encoded, err := sflow.EncodeMessage(packet)
	if err != nil {
		t.Fatalf("EncodeMessage returned error: %v", err)
	}

	decoded, err := (&builtIn{}).decodeSFlow(&event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Payload: append([]byte(nil), encoded...),
	}, encoded, 5)
	if err != nil {
		t.Fatalf("decodeSFlow returned error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 decoded event, got %d", len(decoded))
	}

	fields := decoded[0].Fields
	if got := fields["nat_src_addr"]; got != "203.0.113.10" {
		t.Fatalf("expected nat_src_addr, got %#v", got)
	}
	if got := fields["nat_dst_addr"]; got != "192.0.2.20" {
		t.Fatalf("expected nat_dst_addr, got %#v", got)
	}
	if got := fields["mpls_next_hop_addr"]; got != "192.0.2.254" {
		t.Fatalf("expected mpls_next_hop_addr, got %#v", got)
	}
	if got := fields["mpls_label_1"]; got != uint32(17) {
		t.Fatalf("expected decoded MPLS label 17, got %#v", got)
	}
	if got := fields["mpls_tunnel_lsp_name"]; got != "lsp-a" {
		t.Fatalf("expected mpls_tunnel_lsp_name, got %#v", got)
	}
	records, ok := decoded[0].Internal[sflowFlowRecordsInternalKey].([]sflow.FlowRecord)
	if !ok || len(records) != 3 {
		t.Fatalf("expected 3 preserved extended records, got %#v", decoded[0].Internal[sflowFlowRecordsInternalKey])
	}
}
