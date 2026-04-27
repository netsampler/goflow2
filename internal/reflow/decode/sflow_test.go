package decode

import (
	"bytes"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
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
	if got := fields["message_type"]; got != "counter" {
		t.Fatalf("expected message_type=counter, got %#v", got)
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
