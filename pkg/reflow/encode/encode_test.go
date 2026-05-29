package encode

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/decoders/sflow"
	flowpb "github.com/netsampler/goflow2/v3/pb"
	"github.com/netsampler/goflow2/v3/pkg/reflow/aggregate"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/processor"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestJSONEncoderDropsConfiguredFieldsFromCanonicalOutput(t *testing.T) {
	enc := NewJSONEncoder(config.EncoderConfig{
		Type: "json",
		JSON: config.JSONConfig{
			Flavor:     "canonical",
			DropFields: []string{"header_data"},
		},
	})

	evt := &event.Event{
		Fields: map[string]any{
			"header_data": []byte{0, 1, 2, 3},
			"src_addr":    "192.0.2.10",
		},
	}

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	var decoded map[string]any
	if err := json.Unmarshal(payloads[0], &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	fields, ok := decoded["fields"].(map[string]any)
	if !ok {
		t.Fatalf("expected fields object in payload, got %#v", decoded["fields"])
	}
	if _, exists := fields["header_data"]; exists {
		t.Fatalf("expected header_data to be dropped, got %#v", fields)
	}
	if fields["src_addr"] != "192.0.2.10" {
		t.Fatalf("expected src_addr to be preserved, got %#v", fields["src_addr"])
	}

	if _, exists := evt.Fields["header_data"]; !exists {
		t.Fatalf("expected original event fields to remain unchanged")
	}
}

func TestJSONEncoderGoFlow2V2PrefersNanosecondTimeFields(t *testing.T) {
	enc := NewJSONEncoder(config.EncoderConfig{
		Type: "json",
		JSON: config.JSONConfig{
			Flavor: "goflow2v2",
		},
	})

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"flow_type":          "sflow",
			"start_time_unix":    int64(1_700_000_000_100),
			"end_time_unix":      int64(1_700_000_000_900),
			"time_flow_start_ns": int64(1_700_000_000_100_123_456),
			"time_flow_end_ns":   int64(1_700_000_000_900_123_456),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	dec := json.NewDecoder(bytes.NewReader(payloads[0]))
	dec.UseNumber()
	var decoded map[string]any
	if err := dec.Decode(&decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	numberField := func(key string) int64 {
		t.Helper()
		number, ok := decoded[key].(json.Number)
		if !ok {
			t.Fatalf("expected %s to be json.Number, got %T", key, decoded[key])
		}
		value, err := number.Int64()
		if err != nil {
			t.Fatalf("parse %s: %v", key, err)
		}
		return value
	}

	startNS := numberField("time_flow_start_ns")
	if startNS != 1_700_000_000_100_123_456 {
		t.Fatalf("expected nanosecond start time, got %d", startNS)
	}
	endNS := numberField("time_flow_end_ns")
	if endNS != 1_700_000_000_900_123_456 {
		t.Fatalf("expected nanosecond end time, got %d", endNS)
	}
}

func TestJSONEncoderGoFlow2V2UsesSourceSamplingMetadata(t *testing.T) {
	enc := NewJSONEncoder(config.EncoderConfig{
		Type: "json",
		JSON: config.JSONConfig{
			Flavor: "goflow2v2",
		},
	})

	payloads, err := enc.Encode(&event.Event{
		Source: event.SourceMetadata{
			AgentIP: "198.51.100.99",
			Sampling: &event.SamplingMetadata{
				Rate: 250,
			},
		},
		Fields: map[string]any{
			"agent_ip":      "192.0.2.1",
			"sampling_rate": uint32(100),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	dec := json.NewDecoder(bytes.NewReader(payloads[0]))
	dec.UseNumber()
	var decoded map[string]any
	if err := dec.Decode(&decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded["sampler_address"] != encodeIPBytes("198.51.100.99") {
		t.Fatalf("expected source sampler_address, got %#v", decoded["sampler_address"])
	}
	rate, ok := decoded["sampling_rate"].(json.Number)
	if !ok {
		t.Fatalf("expected sampling_rate json.Number, got %T", decoded["sampling_rate"])
	}
	if got, _ := rate.Int64(); got != 250 {
		t.Fatalf("expected source sampling_rate 250, got %d", got)
	}
}

func TestProtobufEncoderEncodesCanonicalFlowMessage(t *testing.T) {
	enc, err := New(config.EncoderConfig{
		Type: "protobuf",
		Protobuf: config.ProtobufConfig{
			Flavor: "canonical",
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: time.Unix(1, 200).UTC(),
		Source: event.SourceMetadata{
			AgentIP: "192.0.2.1",
			Sampling: &event.SamplingMetadata{
				Rate: 100,
			},
		},
		Fields: map[string]any{
			"flow_type":          "sflow",
			"start_time_unix":    int64(1700000000100),
			"end_time_unix":      int64(1700000000900),
			"time_flow_start_ns": int64(1_700_000_000_100_123_456),
			"time_flow_end_ns":   int64(1_700_000_000_900_123_456),
			"bytes":              int64(321),
			"packets":            int64(7),
			"src_addr":           "192.0.2.10",
			"dst_addr":           "192.0.2.20",
			"proto":              uint32(17),
			"src_port":           uint32(1234),
			"dst_port":           uint32(4321),
			"input_if":           uint32(9),
			"output_if":          uint32(10),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	msg := decodeFlowMessage(t, payloads[0])
	if msg.Type != flowpb.FlowMessage_SFLOW_5 {
		t.Fatalf("expected type SFLOW_5, got %v", msg.Type)
	}
	if msg.SamplingRate != 100 {
		t.Fatalf("expected sampling_rate 100, got %d", msg.SamplingRate)
	}
	if !bytes.Equal(msg.SamplerAddress, []byte{192, 0, 2, 1}) {
		t.Fatalf("expected sampler_address 192.0.2.1, got %v", msg.SamplerAddress)
	}
	if msg.Bytes != 321 || msg.Packets != 7 {
		t.Fatalf("expected bytes=321 packets=7, got bytes=%d packets=%d", msg.Bytes, msg.Packets)
	}
	if msg.TimeFlowStartNs != 1_700_000_000_100_123_456 || msg.TimeFlowEndNs != 1_700_000_000_900_123_456 {
		t.Fatalf("expected nanosecond flow window, got start=%d end=%d", msg.TimeFlowStartNs, msg.TimeFlowEndNs)
	}
	if msg.SrcPort != 1234 || msg.DstPort != 4321 {
		t.Fatalf("expected ports 1234/4321, got %d/%d", msg.SrcPort, msg.DstPort)
	}
}

func TestProtobufEncoderUsesSourceSamplingMetadata(t *testing.T) {
	enc, err := New(config.EncoderConfig{
		Type: "protobuf",
		Protobuf: config.ProtobufConfig{
			Flavor: "canonical",
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		Source: event.SourceMetadata{
			AgentIP: "198.51.100.99",
			Sampling: &event.SamplingMetadata{
				Rate: 250,
			},
		},
		Fields: map[string]any{
			"agent_ip":      "192.0.2.1",
			"sampling_rate": uint32(100),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	msg := decodeFlowMessage(t, payloads[0])
	if msg.SamplingRate != 250 {
		t.Fatalf("expected source sampling_rate 250, got %d", msg.SamplingRate)
	}
	if !bytes.Equal(msg.SamplerAddress, []byte{198, 51, 100, 99}) {
		t.Fatalf("expected source sampler_address 198.51.100.99, got %v", msg.SamplerAddress)
	}
}

func TestProtobufEncoderSupportsGoFlow2V2Flavor(t *testing.T) {
	enc, err := New(config.EncoderConfig{
		Type: "protobuf",
		Protobuf: config.ProtobufConfig{
			Flavor:         "goflow2v2",
			LengthPrefixed: true,
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"flow_type": "netflowv9",
			"src_addr":  "192.0.2.10",
			"dst_addr":  "192.0.2.20",
			"bytes":     int64(321),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	msg := decodeDelimitedFlowMessage(t, payloads[0])
	if msg.Type != flowpb.FlowMessage_NETFLOW_V9 {
		t.Fatalf("expected type NETFLOW_V9, got %v", msg.Type)
	}
	if msg.Bytes != 321 {
		t.Fatalf("expected bytes=321, got %d", msg.Bytes)
	}
}

func TestSFlowEncoderUsesConfiguredAgentIPOverride(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
		SFlow: config.SFlowConfig{
			AgentIP: "203.0.113.10",
		},
	})

	payloads, err := enc.Encode(testSFlowEvent("198.51.100.10"))
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	packet := decodeSFlowPacket(t, payloads[0])
	got, ok := netip.AddrFromSlice(packet.AgentIP)
	if !ok || got.String() != "203.0.113.10" {
		t.Fatalf("expected agent_ip override 203.0.113.10, got %s", got.String())
	}
}

func TestSFlowEncoderUsesAgentIPv6Field(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
	})

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"agent_ipv6":      "2001:db8::10",
			"agent_ip":        "192.0.2.10",
			"protocol":        uint32(1),
			"frame_length":    uint32(60),
			"original_length": uint32(60),
			"header_data":     []byte{0, 1, 2, 3},
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	packet := decodeSFlowPacket(t, payloads[0])
	got, ok := netip.AddrFromSlice(packet.AgentIP)
	if !ok || got.String() != "2001:db8::10" {
		t.Fatalf("expected agent_ipv6 field 2001:db8::10, got %s", got.String())
	}
}

func TestSFlowEncoderIgnoresControlEvents(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{Type: "sflow"})

	payloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "source_init",
			Stream: "options_data",
		},
		Fields: map[string]any{
			"agent_ip":      "192.0.2.1",
			"source_id":     uint32(4),
			"sampling_rate": uint32(1),
			"sample_pool":   uint32(0),
			"drops":         uint32(0),
			"input_if":      uint32(4),
			"output_if":     uint32(4),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 0 {
		t.Fatalf("expected control event to be ignored, got %d payloads", len(payloads))
	}

	payloads, err = enc.Encode(testSFlowEvent("198.51.100.10"))
	if err != nil {
		t.Fatalf("Encode(data) returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected data event to still encode, got %d payloads", len(payloads))
	}
}

func TestSFlowEncoderFallsBackToLoopbackAgentIP(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
	})

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"protocol":        uint32(1),
			"frame_length":    uint32(60),
			"original_length": uint32(60),
			"header_data":     []byte{0, 1, 2, 3},
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	packet := decodeSFlowPacket(t, payloads[0])
	got, ok := netip.AddrFromSlice(packet.AgentIP)
	if !ok || got.String() != "127.0.0.1" {
		t.Fatalf("expected loopback fallback agent_ip 127.0.0.1, got %s", got.String())
	}
}

func TestSFlowEncoderSplitsBatchByAgentIPWhenConfigured(t *testing.T) {
	batchOverAgent := false
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
		Batch: config.BatchConfig{
			Enabled: testBoolPtr(true),
		},
		SFlow: config.SFlowConfig{
			BatchOver: config.SFlowBatchOverConfig{
				AgentIP: &batchOverAgent,
			},
		},
	})

	payloads, err := enc.Encode(testSFlowEvent("198.51.100.10"))
	if err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	}
	if len(payloads) != 0 {
		t.Fatalf("expected no payload on first buffered encode, got %d", len(payloads))
	}

	payloads, err = enc.Encode(testSFlowEvent("198.51.100.20"))
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 flushed payload on incompatible second encode, got %d", len(payloads))
	}
	firstPacket := decodeSFlowPacket(t, payloads[0])
	firstAgent, ok := netip.AddrFromSlice(firstPacket.AgentIP)
	if !ok || firstAgent.String() != "198.51.100.10" {
		t.Fatalf("expected first flushed packet agent_ip 198.51.100.10, got %v", firstPacket.AgentIP)
	}

	payloads, err = enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 remaining payload on final flush, got %d", len(payloads))
	}
	secondPacket := decodeSFlowPacket(t, payloads[0])
	secondAgent, ok := netip.AddrFromSlice(secondPacket.AgentIP)
	if !ok || secondAgent.String() != "198.51.100.20" {
		t.Fatalf("expected second flushed packet agent_ip 198.51.100.20, got %v", secondPacket.AgentIP)
	}
}

func TestSFlowEncoderDropsOversizedSampleWithoutError(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type:             "sflow",
		MaxDatagramBytes: 64,
	})

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"agent_ip":        "192.0.2.1",
			"protocol":        uint32(1),
			"frame_length":    uint32(200),
			"original_length": uint32(200),
			"header_data":     bytes.Repeat([]byte{0xaa}, 200),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error for oversized sample: %v", err)
	}
	if len(payloads) != 0 {
		t.Fatalf("expected oversized sample to be dropped without payload, got %d payloads", len(payloads))
	}
}

func TestSFlowEncoderTruncatesOversizedSampleWhenEnabled(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type:             "sflow",
		MaxDatagramBytes: 96,
		AllowTruncate:    testBoolPtr(true),
	})

	originalHeader := bytes.Repeat([]byte{0xaa}, 200)
	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"agent_ip":        "192.0.2.1",
			"protocol":        uint32(1),
			"frame_length":    uint32(200),
			"original_length": uint32(200),
			"header_data":     originalHeader,
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error for truncatable sample: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one truncated payload, got %d", len(payloads))
	}

	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.FlowSample)
	header := sample.Records[0].Data.(sflow.SampledHeader)
	if len(header.HeaderData) >= len(originalHeader) {
		t.Fatalf("expected truncated header_data length < %d, got %d", len(originalHeader), len(header.HeaderData))
	}
	if int(header.OriginalLength) != len(header.HeaderData) {
		t.Fatalf("expected OriginalLength=%d to match truncated header_data length, got %d", len(header.HeaderData), header.OriginalLength)
	}
}

func TestSFlowEncoderCapsHeadersBeforeBatching(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type:             "sflow",
		MaxDatagramBytes: 400,
		AllowTruncate:    testBoolPtr(true),
		Batch: config.BatchConfig{
			Enabled:    testBoolPtr(true),
			MaxRecords: 2,
		},
		SFlow: config.SFlowConfig{
			MaxHeaderBytes: 64,
		},
	})

	first, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"agent_ip":        "192.0.2.1",
			"protocol":        uint32(1),
			"frame_length":    uint32(200),
			"original_length": uint32(200),
			"header_data":     bytes.Repeat([]byte{0xaa}, 200),
		},
	})
	if err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	}
	if len(first) != 0 {
		t.Fatalf("expected first batched event to stay buffered, got %d payloads", len(first))
	}
	second, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"agent_ip":        "192.0.2.1",
			"protocol":        uint32(1),
			"frame_length":    uint32(200),
			"original_length": uint32(200),
			"header_data":     bytes.Repeat([]byte{0xbb}, 200),
		},
	})
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected one batched payload, got %d", len(second))
	}

	packet := decodeSFlowPacket(t, second[0])
	if len(packet.Samples) != 2 {
		t.Fatalf("expected two samples in one datagram, got %d", len(packet.Samples))
	}
	for i, raw := range packet.Samples {
		sample := raw.(sflow.FlowSample)
		header := sample.Records[0].Data.(sflow.SampledHeader)
		if len(header.HeaderData) != 64 {
			t.Fatalf("sample %d expected capped header_data length 64, got %d", i, len(header.HeaderData))
		}
		if header.OriginalLength != 64 {
			t.Fatalf("sample %d expected original_length/header_size 64, got %d", i, header.OriginalLength)
		}
		if header.FrameLength != 200 {
			t.Fatalf("sample %d expected frame_length 200, got %d", i, header.FrameLength)
		}
	}
}

func TestSFlowEncoderBatchMaxBytesDoesNotUseFieldEstimate(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type:             "sflow",
		MaxDatagramBytes: 4096,
		AllowTruncate:    testBoolPtr(true),
		Batch: config.BatchConfig{
			Enabled:    testBoolPtr(true),
			MaxRecords: 32,
			MaxBytes:   4096,
		},
		SFlow: config.SFlowConfig{
			MaxHeaderBytes: 128,
		},
	})

	for i := 0; i < 3; i++ {
		evt := testSFlowEvent("198.51.100.10")
		for j := 0; j < 128; j++ {
			evt.Fields[fmt.Sprintf("unused_%03d", j)] = "this field is not encoded into sflow"
		}
		payloads, err := enc.Encode(evt)
		if err != nil {
			t.Fatalf("Encode(%d) returned error: %v", i, err)
		}
		if len(payloads) != 0 {
			t.Fatalf("expected event %d to stay buffered, got %d payloads", i, len(payloads))
		}
	}

	payloads, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one flushed sflow packet, got %d", len(payloads))
	}
	packet := decodeSFlowPacket(t, payloads[0])
	if len(packet.Samples) != 3 {
		t.Fatalf("expected three samples after timer-style flush, got %d", len(packet.Samples))
	}
}

func TestSFlowEncoderPacketSequenceAdvancesPerDatagram(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
		Batch: config.BatchConfig{
			Enabled:    testBoolPtr(true),
			MaxRecords: 2,
		},
	})

	firstPayloads, err := enc.Encode(testSFlowEvent("198.51.100.10"))
	if err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	}
	if len(firstPayloads) != 0 {
		t.Fatalf("expected first event to stay buffered, got %d payloads", len(firstPayloads))
	}

	secondPayloads, err := enc.Encode(testSFlowEvent("198.51.100.10"))
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(secondPayloads) != 1 {
		t.Fatalf("expected one flushed payload after second event, got %d", len(secondPayloads))
	}

	thirdPayloads, err := enc.Encode(testSFlowEvent("198.51.100.10"))
	if err != nil {
		t.Fatalf("Encode(third) returned error: %v", err)
	}
	if len(thirdPayloads) != 0 {
		t.Fatalf("expected third event to stay buffered, got %d payloads", len(thirdPayloads))
	}

	fourthPayloads, err := enc.Encode(testSFlowEvent("198.51.100.10"))
	if err != nil {
		t.Fatalf("Encode(fourth) returned error: %v", err)
	}
	if len(fourthPayloads) != 1 {
		t.Fatalf("expected one flushed payload after fourth event, got %d", len(fourthPayloads))
	}

	firstPacket := decodeSFlowPacket(t, secondPayloads[0])
	secondPacket := decodeSFlowPacket(t, fourthPayloads[0])
	if firstPacket.SequenceNumber != 1 {
		t.Fatalf("expected first packet sequence 1, got %d", firstPacket.SequenceNumber)
	}
	if secondPacket.SequenceNumber != 2 {
		t.Fatalf("expected second packet sequence 2, got %d", secondPacket.SequenceNumber)
	}
}

func TestSFlowEncoderDefaultsToOwnedPacketSequence(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{Type: "sflow"})

	first := testSFlowEvent("198.51.100.10")
	first.SFlow.SequenceNumber = 900
	second := testSFlowEvent("198.51.100.10")
	second.SFlow.SequenceNumber = 900

	firstPayloads, err := enc.Encode(first)
	if err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	}
	secondPayloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}

	firstPacket := decodeSFlowPacket(t, firstPayloads[0])
	secondPacket := decodeSFlowPacket(t, secondPayloads[0])
	if firstPacket.SequenceNumber != 1 {
		t.Fatalf("expected first packet sequence 1, got %d", firstPacket.SequenceNumber)
	}
	if secondPacket.SequenceNumber != 2 {
		t.Fatalf("expected second packet sequence 2, got %d", secondPacket.SequenceNumber)
	}
}

func TestSFlowEncoderCanUseMetadataPacketSequence(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
		SFlow: config.SFlowConfig{
			UseMetadataSequenceNumber: true,
		},
	})

	evt := testSFlowEvent("198.51.100.10")
	evt.SFlow.SequenceNumber = 900

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	packet := decodeSFlowPacket(t, payloads[0])
	if packet.SequenceNumber != 900 {
		t.Fatalf("expected packet sequence from metadata 900, got %d", packet.SequenceNumber)
	}
}

func TestSFlowEncoderUsesEventSamplingRate(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
	})

	evt := testSFlowEvent("198.51.100.10")
	evt.SFlow.SamplingRate = 100
	evt.SFlow.SamplePool = 12345
	evt.Fields["sampling_rate"] = uint32(100)
	evt.Fields["sample_pool"] = uint32(12345)

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.FlowSample)
	if sample.SamplingRate != 100 {
		t.Fatalf("expected sampling_rate 100, got %d", sample.SamplingRate)
	}
	if sample.SamplePool != 12345 {
		t.Fatalf("expected sample_pool 12345, got %d", sample.SamplePool)
	}
}

func TestSFlowEncoderUsesSourceSamplingMetadata(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
	})

	evt := testSFlowEvent("198.51.100.10")
	evt.SFlow = nil
	evt.Source.AgentIP = "198.51.100.99"
	evt.Source.SourceID = 42
	evt.Source.Sampling = &event.SamplingMetadata{
		Rate:       250,
		SamplePool: 54321,
		Drops:      7,
	}
	evt.Fields["agent_ip"] = "192.0.2.1"
	evt.Fields["source_id"] = uint32(9)
	evt.Fields["sampling_rate"] = uint32(100)
	evt.Fields["sample_pool"] = uint32(12345)
	evt.Fields["drops"] = uint32(3)

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	packet := decodeSFlowPacket(t, payloads[0])
	gotAgent, ok := netip.AddrFromSlice(packet.AgentIP)
	if !ok || gotAgent.String() != "198.51.100.99" {
		t.Fatalf("expected source agent_ip 198.51.100.99, got %s", gotAgent.String())
	}
	sample := packet.Samples[0].(sflow.FlowSample)
	if sample.Header.SourceIdValue != 42 {
		t.Fatalf("expected source_id 42, got %d", sample.Header.SourceIdValue)
	}
	if sample.SamplingRate != 250 {
		t.Fatalf("expected source sampling_rate 250, got %d", sample.SamplingRate)
	}
	if sample.SamplePool != 54321 {
		t.Fatalf("expected source sample_pool 54321, got %d", sample.SamplePool)
	}
	if sample.Drops != 7 {
		t.Fatalf("expected source drops 7, got %d", sample.Drops)
	}
}

func TestSFlowEncoderBuildsPseudoHeaderFromTuple(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{Type: "sflow"})

	evt := &event.Event{
		Fields: map[string]any{
			"agent_ip":  "192.0.2.10",
			"src_addr":  "192.0.2.1",
			"dst_addr":  "198.51.100.2",
			"src_port":  uint32(12345),
			"dst_port":  uint32(443),
			"proto":     uint32(6),
			"input_if":  uint32(10),
			"output_if": uint32(20),
		},
	}

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.FlowSample)
	header := sample.Records[0].Data.(sflow.SampledHeader)
	if len(header.HeaderData) == 0 {
		t.Fatalf("expected pseudo header data")
	}
	if header.Protocol != 11 {
		t.Fatalf("expected sampled header protocol=11, got %d", header.Protocol)
	}
	if header.FrameLength != uint32(len(header.HeaderData)) {
		t.Fatalf("expected frame length %d, got %d", len(header.HeaderData), header.FrameLength)
	}
	if header.HeaderData[0]>>4 != 4 {
		t.Fatalf("expected direct IPv4 pseudo header, got first byte 0x%02x", header.HeaderData[0])
	}
	if evt.Packet == nil || len(evt.Packet.Layers) == 0 || evt.Packet.Layers[0].Kind != "ipv4" {
		t.Fatalf("expected pseudo packet model to be attached")
	}
}

func TestSFlowEncoderBuildsDirectIPEncapsulatedPseudoHeader(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{Type: "sflow"})

	evt := &event.Event{
		Fields: map[string]any{
			"agent_ip": "192.0.2.10",
		},
		Packet: directGRETCPPacketModel(),
	}

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.FlowSample)
	header := sample.Records[0].Data.(sflow.SampledHeader)
	if header.Protocol != 11 {
		t.Fatalf("expected sampled header protocol=11, got %d", header.Protocol)
	}
	if header.HeaderData[0]>>4 != 4 {
		t.Fatalf("expected direct IPv4 pseudo header, got first byte 0x%02x", header.HeaderData[0])
	}
	if header.HeaderData[9] != 47 {
		t.Fatalf("expected outer IPv4 protocol GRE, got %d", header.HeaderData[9])
	}
	if header.HeaderData[22] != 0x08 || header.HeaderData[23] != 0x00 {
		t.Fatalf("expected GRE inner protocol 0x0800, got %02x%02x", header.HeaderData[22], header.HeaderData[23])
	}
	if evt.Packet == nil || len(evt.Packet.Layers) < 4 || evt.Packet.Layers[0].Kind != "ipv4" {
		t.Fatalf("expected direct nested packet model, got %#v", evt.Packet)
	}
}

func TestSFlowEncoderBuildsDirectIPEncapsulatedPseudoHeaderFromReFlowJSON(t *testing.T) {
	proc := processor.NewBuiltin(config.ProcessorConfig{})
	events, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{
			Type: "json",
			JSON: event.JSONMetadata{Flavor: "reflow"},
		},
		Message: []byte(nestedIPLayersJSON),
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	enc := NewSFlowEncoder(config.EncoderConfig{Type: "sflow"})
	payloads, err := enc.Encode(events[0])
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.FlowSample)
	header := sample.Records[0].Data.(sflow.SampledHeader)
	if header.Protocol != 11 {
		t.Fatalf("expected sampled header protocol=11, got %d", header.Protocol)
	}
	if header.HeaderData[9] != 47 {
		t.Fatalf("expected outer IPv4 protocol GRE, got %d", header.HeaderData[9])
	}
	if header.HeaderData[22] != 0x08 || header.HeaderData[23] != 0x00 {
		t.Fatalf("expected GRE inner protocol 0x0800, got %02x%02x", header.HeaderData[22], header.HeaderData[23])
	}
}

const nestedIPLayersJSON = `{
  "agent_ip": "192.0.2.10",
  "source_id": 1,
  "sampling_rate": 100,
  "input_if": 10,
  "output_if": 20,
  "packet": {
    "layers": [
      {
        "kind": "ipv4",
        "ipv4": {
          "src_addr": "203.0.113.1",
          "dst_addr": "203.0.113.2",
          "protocol": 47,
          "ttl": 64
        }
      },
      {
        "kind": "gre",
        "gre": {
          "protocol": 2048
        }
      },
      {
        "kind": "ipv4",
        "ipv4": {
          "src_addr": "192.0.2.1",
          "dst_addr": "198.51.100.2",
          "protocol": 6,
          "ttl": 64
        }
      },
      {
        "kind": "tcp",
        "tcp": {
          "src_port": 12345,
          "dst_port": 443,
          "flags": 2,
          "window": 65535
        }
      }
    ]
  },
  "bytes": 96,
  "packets": 1,
  "start_time_unix": 1714483200000,
  "end_time_unix": 1714483200100
}`

func TestSFlowEncoderBuildsEncapsulatedPseudoHeader(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{Type: "sflow"})

	evt := &event.Event{
		Fields: map[string]any{
			"agent_ip":        "192.0.2.10",
			"src_mac":         "66:77:88:99:aa:bb",
			"dst_mac":         "00:11:22:33:44:55",
			"vlan_id":         uint32(100),
			"mpls_label":      uint32(17),
			"original_length": uint32(86),
		},
		Packet: encapsulatedGRETCPPacketModel(),
	}

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.FlowSample)
	header := sample.Records[0].Data.(sflow.SampledHeader)
	if len(header.HeaderData) == 0 {
		t.Fatalf("expected encapsulated pseudo header data")
	}
	if header.Protocol != 1 {
		t.Fatalf("expected sampled header protocol=1, got %d", header.Protocol)
	}
	if header.OriginalLength != 86 {
		t.Fatalf("expected original_length=86, got %d", header.OriginalLength)
	}
	if len(header.HeaderData) < 46 {
		t.Fatalf("expected enough header data for ethernet/vlan/mpls/gre, got %d", len(header.HeaderData))
	}
	if header.HeaderData[12] != 0x81 || header.HeaderData[13] != 0x00 {
		t.Fatalf("expected ethernet type 0x8100 for dot1q, got %02x%02x", header.HeaderData[12], header.HeaderData[13])
	}
	if header.HeaderData[16] != 0x88 || header.HeaderData[17] != 0x47 {
		t.Fatalf("expected vlan inner type 0x8847 for mpls, got %02x%02x", header.HeaderData[16], header.HeaderData[17])
	}
	if header.HeaderData[31] != 47 {
		t.Fatalf("expected outer IPv4 protocol GRE, got %d", header.HeaderData[31])
	}
	if header.HeaderData[44] != 0x08 || header.HeaderData[45] != 0x00 {
		t.Fatalf("expected GRE inner protocol 0x0800, got %02x%02x", header.HeaderData[44], header.HeaderData[45])
	}
	if evt.Packet == nil || len(evt.Packet.Layers) < 4 {
		t.Fatalf("expected nested packet model, got %#v", evt.Packet)
	}
}

func TestSFlowEncoderBuildsVXLANPseudoHeaderFromPorts(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{Type: "sflow"})

	evt := &event.Event{
		Fields: map[string]any{
			"agent_ip": "192.0.2.10",
		},
		Packet: vxlanTCPPacketModel(),
	}

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.FlowSample)
	header := sample.Records[0].Data.(sflow.SampledHeader)
	if len(header.HeaderData) == 0 {
		t.Fatalf("expected vxlan pseudo header data")
	}
	if header.Protocol != 11 {
		t.Fatalf("expected sampled header protocol=11, got %d", header.Protocol)
	}
	if evt.Packet == nil || len(evt.Packet.Layers) < 6 {
		t.Fatalf("expected vxlan pseudo packet model, got %#v", evt.Packet)
	}
	if evt.Packet.Layers[1].Kind != "udp" || evt.Packet.Layers[2].Kind != "vxlan" {
		t.Fatalf("expected outer UDP and VXLAN layers, got %#v", evt.Packet.Layers)
	}
}

func directGRETCPPacketModel() *event.PacketModel {
	return &event.PacketModel{
		Layers: []event.LayerSpec{
			ipv4Layer("203.0.113.1", "203.0.113.2", 47),
			{Kind: "gre", GRE: &event.GRELayer{Protocol: 0x0800}},
			ipv4Layer("192.0.2.1", "198.51.100.2", 6),
			tcpLayer(12345, 443),
		},
	}
}

func encapsulatedGRETCPPacketModel() *event.PacketModel {
	return &event.PacketModel{
		Layers: []event.LayerSpec{
			{Kind: "ethernet", Ethernet: &event.EthernetLayer{SrcMAC: "66:77:88:99:aa:bb", DstMAC: "00:11:22:33:44:55"}},
			{Kind: "dot1q", VLAN: &event.VLANLayer{ID: 100, TPID: 0x8100}},
			{Kind: "mpls", MPLS: &event.MPLSLayer{Label: event.MPLSLabel{Label: 17, BOS: true, TTL: 64}}},
			ipv4Layer("203.0.113.1", "203.0.113.2", 47),
			{Kind: "gre", GRE: &event.GRELayer{Protocol: 0x0800}},
			ipv4Layer("192.0.2.1", "198.51.100.2", 6),
			tcpLayer(12345, 443),
		},
	}
}

func vxlanTCPPacketModel() *event.PacketModel {
	return &event.PacketModel{
		Layers: []event.LayerSpec{
			ipv4Layer("203.0.113.1", "203.0.113.2", 17),
			{Kind: "udp", UDP: &event.UDPLayer{SrcPort: 49152, DstPort: 4789}},
			{Kind: "vxlan", VXLAN: &event.VXLANLayer{}},
			{Kind: "ethernet", Ethernet: &event.EthernetLayer{}},
			ipv4Layer("192.0.2.1", "198.51.100.2", 6),
			tcpLayer(12345, 443),
		},
	}
}

func ipv4Layer(src, dst string, proto uint8) event.LayerSpec {
	return event.LayerSpec{
		Kind: "ipv4",
		IPv4: &event.IPv4Layer{
			SrcAddr:  netip.MustParseAddr(src),
			DstAddr:  netip.MustParseAddr(dst),
			Protocol: proto,
			TTL:      64,
		},
	}
}

func tcpLayer(srcPort, dstPort uint16) event.LayerSpec {
	return event.LayerSpec{
		Kind: "tcp",
		TCP: &event.TCPLayer{
			SrcPort: srcPort,
			DstPort: dstPort,
			Flags:   0x02,
			Window:  65535,
		},
	}
}

func TestSFlowEncoderEmitsInterfaceCounterSample(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
	})

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"record_kind":         "interface_counter",
			"agent_ip":            "192.0.2.1",
			"sub_agent_id":        uint32(7),
			"source_id":           uint32(8),
			"if_index":            uint32(9),
			"if_type":             uint32(6),
			"if_speed":            uint64(1000),
			"if_direction":        uint32(1),
			"if_status":           uint32(3),
			"if_in_octets":        uint64(100),
			"if_out_octets":       uint64(200),
			"if_in_errors":        uint32(2),
			"if_out_errors":       uint32(4),
			"if_promiscuous_mode": uint32(1),
		},
		SFlow: &event.SFlowMetadata{
			AgentIP:    "192.0.2.1",
			SubAgentID: 7,
			SourceID:   8,
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	packet := decodeSFlowPacket(t, payloads[0])
	sample, ok := packet.Samples[0].(sflow.CounterSample)
	if !ok {
		t.Fatalf("expected counter sample, got %T", packet.Samples[0])
	}
	if sample.Header.SourceIdValue != 8 {
		t.Fatalf("expected source id 8, got %d", sample.Header.SourceIdValue)
	}
	if sample.CounterRecordsCount != 1 {
		t.Fatalf("expected 1 counter record, got %d", sample.CounterRecordsCount)
	}
	record, ok := sample.Records[0].Data.(sflow.IfCounters)
	if !ok {
		t.Fatalf("expected IfCounters record, got %T", sample.Records[0].Data)
	}
	if record.IfIndex != 9 {
		t.Fatalf("expected if_index 9, got %d", record.IfIndex)
	}
	if record.IfOutOctets != 200 {
		t.Fatalf("expected if_out_octets 200, got %d", record.IfOutOctets)
	}
}

func TestSFlowEncoderUsesConfiguredExpandedCounterFormat(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
		SFlow: config.SFlowConfig{
			CounterFormat: "expanded",
		},
	})

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"record_kind":    "interface_counter",
			"agent_ip":       "192.0.2.1",
			"source_id":      uint32(8),
			"source_id_type": uint32(2),
			"if_index":       uint32(9),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.CounterSample)
	if sample.Header.Format != sflow.SAMPLE_FORMAT_EXPANDED_COUNTER {
		t.Fatalf("expected expanded counter sample format, got %d", sample.Header.Format)
	}
	if sample.Header.SourceIdType != 2 {
		t.Fatalf("expected source_id_type 2, got %d", sample.Header.SourceIdType)
	}
}

func TestSFlowCounterEventOverridesConfiguredFormat(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
		SFlow: config.SFlowConfig{
			CounterFormat: "standard",
		},
	})

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"record_kind":    "interface_counter",
			"counter_format": "expanded",
			"agent_ip":       "192.0.2.1",
			"source_id":      uint32(8),
			"source_id_type": uint32(3),
			"if_index":       uint32(9),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	packet := decodeSFlowPacket(t, payloads[0])
	sample := packet.Samples[0].(sflow.CounterSample)
	if sample.Header.Format != sflow.SAMPLE_FORMAT_EXPANDED_COUNTER {
		t.Fatalf("expected event override to force expanded format, got %d", sample.Header.Format)
	}
	if sample.Header.SourceIdType != 3 {
		t.Fatalf("expected source_id_type 3, got %d", sample.Header.SourceIdType)
	}
}

func TestIPFIXEncoderEmitsTemplateAndDataRecord(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.ObservationDomainID = 42
	enc := NewIPFIXEncoder(cfg)
	evt := testTemplatedFlowEvent()
	evt.Fields["observation_domain_id"] = uint32(42)

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}

	if decoded.ObservationDomainId != 42 {
		t.Fatalf("expected observation domain 42, got %d", decoded.ObservationDomainId)
	}
	if decoded.SequenceNumber != 0 {
		t.Fatalf("expected sequence 0, got %d", decoded.SequenceNumber)
	}
	if len(decoded.FlowSets) != 2 {
		t.Fatalf("expected 2 flow sets, got %d", len(decoded.FlowSets))
	}

	dataSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	if got := dataSet.Records[0].Values[0].Value.([]byte); !bytes.Equal(got, []byte{192, 0, 2, 10}) {
		t.Fatalf("expected src_addr bytes 192.0.2.10, got %v", got)
	}
}

func TestTemplatedFlowEncoderExportsFlowDirection(t *testing.T) {
	tests := []struct {
		name      string
		typ       string
		fieldType uint16
	}{
		{name: "ipfix", typ: "ipfix", fieldType: netflow.IPFIX_FIELD_flowDirection},
		{name: "netflowv9", typ: "netflowv9", fieldType: netflow.NFV9_FIELD_DIRECTION},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testTFlowEncoderConfig(tt.typ)
			cfg.TemplatedFlow.Data.Select = []string{"flow_direction"}
			cfg.TemplatedFlow.Data.Catalog["flow_direction"] = config.IPFIXFieldDefinition{
				ID:     netflow.IPFIX_FIELD_flowDirection,
				Length: 1,
				Type:   "unsigned8",
			}
			evt := testTemplatedFlowEvent()
			evt.Fields["flow_direction"] = uint32(1)

			var payloads [][]byte
			var err error
			store := templates.NewTemplateFlowStore()
			store.Start()
			switch tt.typ {
			case "ipfix":
				payloads, err = NewIPFIXEncoder(cfg).Encode(evt)
				if err != nil {
					t.Fatalf("Encode returned error: %v", err)
				}
				var decoded netflow.IPFIXPacket
				if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
					t.Fatalf("decode ipfix payload: %v", err)
				}
				assertFlowDirectionTemplateAndValue(t, decoded.FlowSets, tt.fieldType)
			case "netflowv9":
				payloads, err = NewNFv9Encoder(cfg).Encode(evt)
				if err != nil {
					t.Fatalf("Encode returned error: %v", err)
				}
				var decoded netflow.NFv9Packet
				if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
					t.Fatalf("decode netflow v9 payload: %v", err)
				}
				assertFlowDirectionTemplateAndValue(t, decoded.FlowSets, tt.fieldType)
			}
		})
	}
}

func TestIPFIXEncoderBatchesCompatibleDataRecords(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
	}
	enc := NewIPFIXEncoder(cfg)

	firstPayloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	}
	if len(firstPayloads) != 0 {
		t.Fatalf("expected first IPFIX record to stay buffered, got %d payloads", len(firstPayloads))
	}
	second := testTemplatedFlowEvent()
	second.Fields["bytes"] = int64(654)
	secondPayloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(secondPayloads) != 1 {
		t.Fatalf("expected batched IPFIX payload, got %d", len(secondPayloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(secondPayloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}
	if decoded.SequenceNumber != 0 {
		t.Fatalf("expected first batched packet sequence 0, got %d", decoded.SequenceNumber)
	}
	if len(decoded.FlowSets) != 2 {
		t.Fatalf("expected template and data flow sets, got %d", len(decoded.FlowSets))
	}
	dataSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	if len(dataSet.Records) != 2 {
		t.Fatalf("expected two IPFIX data records, got %d", len(dataSet.Records))
	}

	thirdPayloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode(third) returned error: %v", err)
	}
	if len(thirdPayloads) != 0 {
		t.Fatalf("expected third IPFIX record to stay buffered, got %d payloads", len(thirdPayloads))
	}
	flushed, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(flushed) != 1 {
		t.Fatalf("expected one flushed IPFIX payload, got %d", len(flushed))
	}
	var flushedDecoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(flushed[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &flushedDecoded); err != nil {
		t.Fatalf("decode flushed ipfix payload: %v", err)
	}
	if flushedDecoded.SequenceNumber != 2 {
		t.Fatalf("expected flushed packet sequence 2, got %d", flushedDecoded.SequenceNumber)
	}
}

func TestIPFIXEncoderBatchMaxBytesUsesRenderedDataFields(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 32,
		MaxBytes:   100,
	}
	enc := NewIPFIXEncoder(cfg)

	for i := 0; i < 3; i++ {
		evt := testTemplatedFlowEvent()
		evt.Fields["bytes"] = int64(i)
		for j := 0; j < 128; j++ {
			evt.Fields[fmt.Sprintf("unused_%03d", j)] = "this field is not encoded into ipfix"
		}
		payloads, err := enc.Encode(evt)
		if err != nil {
			t.Fatalf("Encode(%d) returned error: %v", i, err)
		}
		if len(payloads) != 0 {
			t.Fatalf("expected event %d to stay buffered, got %d payloads", i, len(payloads))
		}
	}
	if enc.estimatedBytes != 87 {
		t.Fatalf("expected rendered field estimate 87, got %d", enc.estimatedBytes)
	}

	payloads, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one flushed IPFIX packet, got %d", len(payloads))
	}
	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}
	dataSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	if len(dataSet.Records) != 3 {
		t.Fatalf("expected three IPFIX data records, got %d", len(dataSet.Records))
	}
}

func TestIPFIXEncoderCapacityFlushKeepsSmallTailBuffered(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.MaxDatagramBytes = 150
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 32,
		MaxBytes:   100,
	}
	enc := NewIPFIXEncoder(cfg)

	var payloads [][]byte
	for i := 0; i < 4; i++ {
		evt := testTemplatedFlowEvent()
		evt.Fields["bytes"] = int64(i)
		got, err := enc.Encode(evt)
		if err != nil {
			t.Fatalf("Encode(%d) returned error: %v", i, err)
		}
		payloads = append(payloads, got...)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected capacity flush to emit one packet and keep the tail buffered, got %d payloads", len(payloads))
	}
	if len(enc.events) == 0 {
		t.Fatalf("expected small tail to remain buffered")
	}

	flushed, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(flushed) != 1 {
		t.Fatalf("expected timer/shutdown flush to emit buffered tail, got %d payloads", len(flushed))
	}
}

func TestIPFIXEncoderPayloadBatchFlowSetLengthsAreConsistent(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.Data.Catalog["frame_length"] = config.IPFIXFieldDefinition{ID: 312, Length: 2, Type: "unsigned16"}
	cfg.TemplatedFlow.Data.Catalog["header_data"] = config.IPFIXFieldDefinition{ID: 315, Length: 0xffff, Type: "bytes"}
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
		MaxBytes:   1200,
	}
	enc := NewIPFIXEncoder(cfg)

	if _, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream: "flow_data",
			Fields: []event.SchemaField{
				{Role: "key", Name: "src_addr"},
				{Role: "key", Name: "dst_addr"},
				{Role: "current", Name: "frame_length"},
				{Role: "current", Name: "header_data"},
			},
			BaseTemplateID: 256,
		},
	}); err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}

	first := testTemplatedFlowEvent()
	firstHeader := append(bytes.Repeat([]byte{0xff}, 6), bytes.Repeat([]byte{0xab}, 90)...)
	first.Fields["frame_length"] = uint32(0xffff)
	first.Fields["header_data"] = firstHeader
	if payloads, err := enc.Encode(first); err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	} else if len(payloads) != 0 {
		t.Fatalf("expected first payload record to stay buffered, got %d payloads", len(payloads))
	}

	second := testTemplatedFlowEvent()
	second.Fields["src_addr"] = "2001:db8::10"
	second.Fields["dst_addr"] = "2001:db8::20"
	second.Fields["frame_length"] = uint32(300)
	second.Fields["header_data"] = bytes.Repeat([]byte{0xcd}, 300)
	payloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one batched payload, got %d", len(payloads))
	}

	firstSetLength := int(binary.BigEndian.Uint16(payloads[0][18:20]))
	firstHeaderOffset := 16 + 4 + 4 + 4 + 2
	if payloads[0][firstHeaderOffset] != byte(len(firstHeader)) {
		t.Fatalf("expected first header_data short length prefix %d at offset %d, got %#x", len(firstHeader), firstHeaderOffset, payloads[0][firstHeaderOffset])
	}
	if got := payloads[0][firstHeaderOffset+1 : firstHeaderOffset+7]; !bytes.Equal(got, bytes.Repeat([]byte{0xff}, 6)) {
		t.Fatalf("expected broadcast header_data after length prefix, got %x", got)
	}
	if got := payloads[0][16+firstSetLength-1 : 16+firstSetLength]; !bytes.Equal(got, []byte{0}) {
		t.Fatalf("expected first data set padding before next set, got %x", got)
	}

	packetLength := int(binary.BigEndian.Uint16(payloads[0][2:4]))
	if packetLength != len(payloads[0]) {
		t.Fatalf("expected IPFIX packet length %d to equal payload bytes %d", packetLength, len(payloads[0]))
	}
	offset := 16
	for flowSet := 0; offset < len(payloads[0]); flowSet++ {
		if offset+4 > len(payloads[0]) {
			t.Fatalf("flowset %d header at offset %d exceeds payload length %d", flowSet, offset, len(payloads[0]))
		}
		length := int(binary.BigEndian.Uint16(payloads[0][offset+2 : offset+4]))
		if length < 4 {
			t.Fatalf("flowset %d at offset %d has invalid length %d", flowSet, offset, length)
		}
		if offset+length > len(payloads[0]) {
			t.Fatalf("flowset %d at offset %d length %d exceeds payload length %d", flowSet, offset, length, len(payloads[0]))
		}
		offset += length
	}
	if offset != len(payloads[0]) {
		t.Fatalf("expected flowset lengths to end at %d, got %d", len(payloads[0]), offset)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}
	templatePayloads, err := enc.encodeSchemaTemplates(enc.dataSchemas["flow_data"])
	if err != nil {
		t.Fatalf("encodeSchemaTemplates returned error: %v", err)
	}
	for _, payload := range templatePayloads {
		var templatePacket netflow.IPFIXPacket
		if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), store, ctx, nil, &templatePacket); err != nil {
			t.Fatalf("decode schema template payload: %v", err)
		}
	}
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, ctx, nil, &decoded); err != nil {
		t.Fatalf("decode batched payload: %v", err)
	}
	if len(decoded.FlowSets) != 2 {
		t.Fatalf("expected two data sets, got %d", len(decoded.FlowSets))
	}
	firstSet := decoded.FlowSets[0].(netflow.DataFlowSet)
	values := firstSet.Records[0].Values
	if got := values[len(values)-1].Value.([]byte); !bytes.Equal(got, firstHeader) {
		t.Fatalf("expected decoded broadcast header_data length %d, got %d", len(firstHeader), len(got))
	}
}

func TestIPFIXEncoderFallbackUsesConfiguredSelectWidth(t *testing.T) {
	enc := NewIPFIXEncoder(testTFlowEncoderConfig("ipfix"))
	evt := testTemplatedFlowEvent()
	delete(evt.Fields, "bytes")
	delete(evt.Fields, "packets")

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one IPFIX payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}
	if len(decoded.FlowSets) != 2 {
		t.Fatalf("expected template and data flow sets, got %d", len(decoded.FlowSets))
	}
	templateSet, ok := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	if !ok {
		t.Fatalf("expected first flow set to be TemplateFlowSet, got %T", decoded.FlowSets[0])
	}
	if templateSet.Records[0].FieldCount != 7 {
		t.Fatalf("expected configured 7-field template, got %d", templateSet.Records[0].FieldCount)
	}
	dataSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	values := dataSet.Records[0].Values
	if len(values) != 7 {
		t.Fatalf("expected configured 7-value data record, got %d", len(values))
	}
	if got := values[5].Value.([]byte); !bytes.Equal(got, make([]byte, 8)) {
		t.Fatalf("expected missing bytes field to default to zero, got %v", got)
	}
	if got := values[6].Value.([]byte); !bytes.Equal(got, make([]byte, 8)) {
		t.Fatalf("expected missing packets field to default to zero, got %v", got)
	}
}

func TestIPFIXEncoderBatchesMultipleDataSetsInOnePacket(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
	}
	enc := NewIPFIXEncoder(cfg)

	firstPayloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	}
	if len(firstPayloads) != 0 {
		t.Fatalf("expected first IPFIX record to stay buffered, got %d payloads", len(firstPayloads))
	}

	second := testTemplatedFlowEvent()
	second.Fields["template_id"] = uint32(257)
	secondPayloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(secondPayloads) != 1 {
		t.Fatalf("expected one batched IPFIX payload, got %d", len(secondPayloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(secondPayloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}
	if decoded.SequenceNumber != 0 {
		t.Fatalf("expected first batched packet sequence 0, got %d", decoded.SequenceNumber)
	}
	if len(decoded.FlowSets) != 3 {
		t.Fatalf("expected template set and two data sets, got %d", len(decoded.FlowSets))
	}
	templateSet, ok := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	if !ok {
		t.Fatalf("expected first flow set to be TemplateFlowSet, got %T", decoded.FlowSets[0])
	}
	if len(templateSet.Records) != 2 {
		t.Fatalf("expected two template records, got %d", len(templateSet.Records))
	}
	firstSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	secondSet, ok := decoded.FlowSets[2].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected third flow set to be DataFlowSet, got %T", decoded.FlowSets[2])
	}
	if firstSet.Id != 256 || secondSet.Id != 257 {
		t.Fatalf("expected data set ids 256 and 257, got %d and %d", firstSet.Id, secondSet.Id)
	}
	if len(firstSet.Records) != 1 || len(secondSet.Records) != 1 {
		t.Fatalf("expected one record per data set, got %d and %d", len(firstSet.Records), len(secondSet.Records))
	}

	if payloads, err := enc.Encode(testTemplatedFlowEvent()); err != nil {
		t.Fatalf("Encode(third) returned error: %v", err)
	} else if len(payloads) != 0 {
		t.Fatalf("expected third IPFIX record to stay buffered, got %d payloads", len(payloads))
	}
	flushed, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(flushed) != 1 {
		t.Fatalf("expected one flushed IPFIX payload, got %d", len(flushed))
	}
	var flushedDecoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(flushed[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &flushedDecoded); err != nil {
		t.Fatalf("decode flushed ipfix payload: %v", err)
	}
	if flushedDecoded.SequenceNumber != 2 {
		t.Fatalf("expected flushed packet sequence 2 after two data records, got %d", flushedDecoded.SequenceNumber)
	}
}

func TestIPFIXEncoderBatchesSchemaDataSetsInOnePacket(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
	}
	enc := NewIPFIXEncoder(cfg)

	templatePayloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"src_addr", "dst_addr", "src_port", "dst_port", "proto", "bytes", "packets"},
			BaseTemplateID: 256,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}
	for _, payload := range templatePayloads {
		var templatePacket netflow.IPFIXPacket
		if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), store, ctx, nil, &templatePacket); err != nil {
			t.Fatalf("decode schema template payload: %v", err)
		}
	}

	if payloads, err := enc.Encode(testTemplatedFlowEvent()); err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	} else if len(payloads) != 0 {
		t.Fatalf("expected first schema record to stay buffered, got %d payloads", len(payloads))
	}

	second := testTemplatedFlowEvent()
	second.Fields["src_addr"] = "2001:db8::10"
	second.Fields["dst_addr"] = "2001:db8::20"
	payloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one schema batched IPFIX payload, got %d", len(payloads))
	}

	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, ctx, nil, &decoded); err != nil {
		t.Fatalf("decode schema data payload: %v", err)
	}
	if len(decoded.FlowSets) != 2 {
		t.Fatalf("expected two schema data sets, got %d", len(decoded.FlowSets))
	}
	firstSet, ok := decoded.FlowSets[0].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected first flow set to be DataFlowSet, got %T", decoded.FlowSets[0])
	}
	secondSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	if firstSet.Id == secondSet.Id {
		t.Fatalf("expected different data set ids, got %d", firstSet.Id)
	}
	if len(firstSet.Records) != 1 || len(secondSet.Records) != 1 {
		t.Fatalf("expected one record per schema data set, got %d and %d", len(firstSet.Records), len(secondSet.Records))
	}
}

func TestIPFIXEncoderBatchesFallbackAddressFamiliesWithDistinctTemplateIDs(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
	}
	enc := NewIPFIXEncoder(cfg)

	if payloads, err := enc.Encode(testTemplatedFlowEvent()); err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	} else if len(payloads) != 0 {
		t.Fatalf("expected first IPFIX record to stay buffered, got %d payloads", len(payloads))
	}

	second := testTemplatedFlowEvent()
	second.Fields["src_addr"] = "2001:db8::10"
	second.Fields["dst_addr"] = "2001:db8::20"
	payloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one mixed IPv4/IPv6 payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, ctx, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}
	if len(decoded.FlowSets) != 3 {
		t.Fatalf("expected template set plus IPv4/IPv6 data sets, got %d", len(decoded.FlowSets))
	}
	templateSet, ok := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	if !ok {
		t.Fatalf("expected first flow set to be TemplateFlowSet, got %T", decoded.FlowSets[0])
	}
	if len(templateSet.Records) != 2 {
		t.Fatalf("expected two address-family templates, got %d", len(templateSet.Records))
	}
	if templateSet.Records[0].TemplateId != 256 || templateSet.Records[1].TemplateId != 257 {
		t.Fatalf("expected IPv4/IPv6 template ids 256/257, got %d/%d", templateSet.Records[0].TemplateId, templateSet.Records[1].TemplateId)
	}
	if templateSet.Records[0].Fields[0].Type != netflow.IPFIX_FIELD_sourceIPv4Address {
		t.Fatalf("expected first template sourceIPv4Address, got %d", templateSet.Records[0].Fields[0].Type)
	}
	if templateSet.Records[1].Fields[0].Type != netflow.IPFIX_FIELD_sourceIPv6Address {
		t.Fatalf("expected second template sourceIPv6Address, got %d", templateSet.Records[1].Fields[0].Type)
	}
	firstSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	secondSet, ok := decoded.FlowSets[2].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected third flow set to be DataFlowSet, got %T", decoded.FlowSets[2])
	}
	if firstSet.Id != 256 || secondSet.Id != 257 {
		t.Fatalf("expected IPv4/IPv6 data set ids 256/257, got %d/%d", firstSet.Id, secondSet.Id)
	}
}

func TestIPFIXEncoderPreservesBufferedEventsOnBatchError(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
	}
	enc := NewIPFIXEncoder(cfg)
	first := testTemplatedFlowEvent()
	bad := &event.Event{
		ReceivedAt: time.Unix(1, 0).UTC(),
		Fields: map[string]any{
			"unmapped_field": uint32(1),
		},
	}

	payloads, err := enc.Encode(first)
	if err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	}
	if len(payloads) != 0 {
		t.Fatalf("expected first record to stay buffered, got %d payloads", len(payloads))
	}
	payloads, err = enc.Encode(bad)
	if err == nil {
		t.Fatalf("expected bad batch event to return error")
	}
	if len(payloads) != 0 {
		t.Fatalf("expected no payloads from failed batch, got %d", len(payloads))
	}
	if len(enc.events) != 2 {
		t.Fatalf("expected failed batch to preserve 2 buffered events, got %d", len(enc.events))
	}
	if enc.events[0] != first || enc.events[1] != bad {
		t.Fatalf("expected buffered events to remain in original order")
	}
	if enc.estimatedBytes == 0 {
		t.Fatalf("expected estimated byte count to be preserved")
	}
}

func TestIPFIXEncoderIgnoresEventObservationDomainID(t *testing.T) {
	enc := NewIPFIXEncoder(testTFlowEncoderConfig("ipfix"))
	evt := testTemplatedFlowEvent()
	evt.Fields["observation_domain_id"] = uint32(777)

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}
	if decoded.ObservationDomainId != 0 {
		t.Fatalf("expected exporter observation domain 0, got %d", decoded.ObservationDomainId)
	}
}

func TestIPFIXEncoderConfigObservationDomainIDOverridesEvent(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.ObservationDomainID = 888
	enc := NewIPFIXEncoder(cfg)
	evt := testTemplatedFlowEvent()
	evt.Fields["observation_domain_id"] = uint32(777)
	evt.Fields["source_id"] = uint32(42)

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}
	if decoded.ObservationDomainId != 888 {
		t.Fatalf("expected observation domain 888, got %d", decoded.ObservationDomainId)
	}
}

func TestIPFIXDataRecordUsesSourceIDAsObservationPointID(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.Data.Select = []string{"source_id", "bytes"}
	cfg.TemplatedFlow.Data.Catalog["source_id"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_observationPointId, Length: 8, Type: "unsigned64"}
	enc := NewIPFIXEncoder(cfg)

	evt := testTemplatedFlowEvent()
	delete(evt.Fields, "source_id")
	evt.Source.SourceID = 0
	evt.Source.SourceIDSet = true
	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}
	templateSet, ok := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	if !ok {
		t.Fatalf("expected template flow set, got %T", decoded.FlowSets[0])
	}
	if templateSet.Records[0].Fields[0].Type != netflow.IPFIX_FIELD_observationPointId {
		t.Fatalf("expected observationPointId template field, got %d", templateSet.Records[0].Fields[0].Type)
	}
	dataSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected data flow set, got %T", decoded.FlowSets[1])
	}
	if got := dataSet.Records[0].Values[0].Value.([]byte); !bytes.Equal(got, encodeU64(0)) {
		t.Fatalf("expected observation point value 0, got %v", got)
	}
}

func TestIPFIXSourceOptionsUseObservationPointScope(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.ObservationDomainID = 888
	enc := NewIPFIXEncoder(cfg)

	payloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type: "source_init",
		},
		Source: event.SourceMetadata{
			AgentIP:  "198.51.100.99",
			SourceID: 42,
			Sampling: &event.SamplingMetadata{
				Rate: 250,
			},
		},
		Fields: map[string]any{
			"observation_domain_id": uint32(777),
		},
		Payload: event.SourceInit{
			ObservationDomainID: 777,
			SourceID:            42,
			SamplingRate:        100,
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one options payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix source options payload: %v", err)
	}
	if decoded.ObservationDomainId != 888 {
		t.Fatalf("expected packet observation domain 888, got %d", decoded.ObservationDomainId)
	}
	optionsTemplate, ok := decoded.FlowSets[0].(netflow.IPFIXOptionsTemplateFlowSet)
	if !ok {
		t.Fatalf("expected options template flow set, got %T", decoded.FlowSets[0])
	}
	scopeField := optionsTemplate.Records[0].Scopes[0]
	if scopeField.Type != netflow.IPFIX_FIELD_observationPointId {
		t.Fatalf("expected observationPointId scope field, got %d", scopeField.Type)
	}
	if scopeField.Length != 8 {
		t.Fatalf("expected observationPointId scope length 8, got %d", scopeField.Length)
	}
	optionsData, ok := decoded.FlowSets[1].(netflow.OptionsDataFlowSet)
	if !ok {
		t.Fatalf("expected options data flow set, got %T", decoded.FlowSets[1])
	}
	scope := optionsData.Records[0].ScopesValues[0]
	if scope.Type != netflow.IPFIX_FIELD_observationPointId {
		t.Fatalf("expected observationPointId scope, got %d", scope.Type)
	}
	if got := scope.Value.([]byte); !bytes.Equal(got, encodeU64(42)) {
		t.Fatalf("expected options observation point scope 42, got %v", got)
	}
	option := optionsData.Records[0].OptionsValues[0]
	if got := option.Value.([]byte); !bytes.Equal(got, encodeU32(250)) {
		t.Fatalf("expected source metadata sampling rate 250, got %v", got)
	}

	dataPayloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode data returned error: %v", err)
	}
	if len(dataPayloads) != 1 {
		t.Fatalf("expected one data payload, got %d", len(dataPayloads))
	}
	var dataDecoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(dataPayloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &dataDecoded); err != nil {
		t.Fatalf("decode ipfix data payload: %v", err)
	}
	if dataDecoded.SequenceNumber != 1 {
		t.Fatalf("expected first data packet after options data record sequence 1, got %d", dataDecoded.SequenceNumber)
	}
}

func TestIPFIXAggregationSchemaOptionsDataUsesKeysAsScopes(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.ObservationDomainID = 888
	cfg.TemplatedFlow.Data.Catalog["observation_domain_id"] = config.IPFIXFieldDefinition{ID: 149, Length: 4, Type: "unsigned32"}
	cfg.TemplatedFlow.Data.Catalog["if_index"] = config.IPFIXFieldDefinition{ID: 10, Length: 4, Type: "unsigned32"}
	cfg.TemplatedFlow.Data.Catalog["if_name"] = config.IPFIXFieldDefinition{ID: 82, Length: 0xffff, Type: "string"}
	enc := NewIPFIXEncoder(cfg)

	schemaPayloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "interface_options",
		},
		Payload: event.AggregationSchema{
			Stream: "interface_options",
			Fields: []event.SchemaField{
				{Role: "static", Name: "tflow_record_type", Value: "options"},
				{Role: "key", Name: "observation_domain_id"},
				{Role: "key", Name: "if_index"},
				{Role: "current", Name: "if_name"},
			},
			BaseTemplateID: 1300,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}
	if len(schemaPayloads) != 1 {
		t.Fatalf("expected one options schema payload, got %d", len(schemaPayloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}
	var schemaDecoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(schemaPayloads[0]), store, ctx, nil, &schemaDecoded); err != nil {
		t.Fatalf("decode options schema payload: %v", err)
	}
	optionsTemplate, ok := schemaDecoded.FlowSets[0].(netflow.IPFIXOptionsTemplateFlowSet)
	if !ok {
		t.Fatalf("expected options template flow set, got %T", schemaDecoded.FlowSets[0])
	}
	record := optionsTemplate.Records[0]
	if record.TemplateId != 1300 || record.FieldCount != 3 || record.ScopeFieldCount != 2 {
		t.Fatalf("unexpected options template counts: %#v", record)
	}
	if record.Scopes[0].Type != 149 || record.Scopes[1].Type != 10 || record.Options[0].Type != 82 {
		t.Fatalf("unexpected options template fields: scopes=%#v options=%#v", record.Scopes, record.Options)
	}

	dataPayloads, err := enc.Encode(&event.Event{
		ReceivedAt: testEventTime(),
		Stream:     "interface_options",
		Fields: map[string]any{
			"observation_domain_id": uint32(777),
			"if_index":              uint32(2),
			"if_name":               "eth0",
		},
	})
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}
	if len(dataPayloads) != 1 {
		t.Fatalf("expected one options data payload, got %d", len(dataPayloads))
	}
	var dataDecoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(dataPayloads[0]), store, ctx, nil, &dataDecoded); err != nil {
		t.Fatalf("decode options data payload: %v", err)
	}
	optionsData, ok := dataDecoded.FlowSets[0].(netflow.OptionsDataFlowSet)
	if !ok {
		t.Fatalf("expected options data flow set, got %T", dataDecoded.FlowSets[0])
	}
	values := optionsData.Records[0]
	if !bytes.Equal(values.ScopesValues[0].Value.([]byte), encodeU32(777)) || !bytes.Equal(values.ScopesValues[1].Value.([]byte), encodeU32(2)) {
		t.Fatalf("unexpected options scope values: %#v", values.ScopesValues)
	}
	if !bytes.Equal(values.OptionsValues[0].Value.([]byte), []byte("eth0")) {
		t.Fatalf("expected interface name option eth0, got %#v", values.OptionsValues[0].Value)
	}
}

func TestIPFIXSourceOptionsRefreshMergesObservationPointRecords(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.ObservationDomainID = 888
	cfg.TemplatedFlow.OptionsRefresh = 1
	enc := NewIPFIXEncoder(cfg)

	for _, sourceID := range []uint32{42, 43} {
		if _, err := enc.Encode(&event.Event{
			Kind: "control",
			Control: &event.ControlMetadata{
				Type: "source_init",
			},
			Source: event.SourceMetadata{
				SourceID: sourceID,
				Sampling: &event.SamplingMetadata{
					Rate: sourceID + 100,
				},
			},
		}); err != nil {
			t.Fatalf("Encode source_init %d returned error: %v", sourceID, err)
		}
	}
	if len(enc.sourceOptions) != 2 {
		t.Fatalf("expected two source options states, got %d", len(enc.sourceOptions))
	}

	enc.lastOptionsRun = time.Time{}
	payloads, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one merged source options payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode merged source options payload: %v", err)
	}
	optionsData, ok := decoded.FlowSets[1].(netflow.OptionsDataFlowSet)
	if !ok {
		t.Fatalf("expected options data flow set, got %T", decoded.FlowSets[1])
	}
	if len(optionsData.Records) != 2 {
		t.Fatalf("expected two merged options data records, got %d", len(optionsData.Records))
	}
	for i, wantSourceID := range []uint64{42, 43} {
		if got := optionsData.Records[i].ScopesValues[0].Value.([]byte); !bytes.Equal(got, encodeU64(wantSourceID)) {
			t.Fatalf("expected options record %d observation point %d, got %v", i, wantSourceID, got)
		}
	}
}

func TestIPFIXSourceInitBatchSendsOneTemplateAndMultipleDataRecords(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.ObservationDomainID = 888
	enc := NewIPFIXEncoder(cfg)

	payloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type: "source_init_batch",
		},
		Payload: []*event.Event{
			{
				Source: event.SourceMetadata{
					SourceID:    0,
					SourceIDSet: true,
					Sampling: &event.SamplingMetadata{
						Rate: 100,
					},
				},
			},
			{
				Source: event.SourceMetadata{
					SourceID:    1,
					SourceIDSet: true,
					Sampling: &event.SamplingMetadata{
						Rate: 200,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one source-init batch payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode source-init batch payload: %v", err)
	}
	optionsTemplate, ok := decoded.FlowSets[0].(netflow.IPFIXOptionsTemplateFlowSet)
	if !ok {
		t.Fatalf("expected options template flow set, got %T", decoded.FlowSets[0])
	}
	if len(optionsTemplate.Records) != 1 || optionsTemplate.Records[0].TemplateId != 1024 {
		t.Fatalf("expected one options template 1024, got %#v", optionsTemplate.Records)
	}
	optionsData, ok := decoded.FlowSets[1].(netflow.OptionsDataFlowSet)
	if !ok {
		t.Fatalf("expected options data flow set, got %T", decoded.FlowSets[1])
	}
	if len(optionsData.Records) != 2 {
		t.Fatalf("expected two options data records, got %d", len(optionsData.Records))
	}
	for i, wantSourceID := range []uint64{0, 1} {
		if got := optionsData.Records[i].ScopesValues[0].Value.([]byte); !bytes.Equal(got, encodeU64(wantSourceID)) {
			t.Fatalf("expected options data record %d observation point %d, got %v", i, wantSourceID, got)
		}
	}
}

func TestNFv9EncoderEmitsTemplateAndDataRecord(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.TemplatedFlow.ObservationDomainID = 42
	enc := NewNFv9Encoder(cfg)

	payloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 payload: %v", err)
	}

	if decoded.SourceId != 42 {
		t.Fatalf("expected source id 42, got %d", decoded.SourceId)
	}
	if decoded.SequenceNumber != 0 {
		t.Fatalf("expected sequence 0, got %d", decoded.SequenceNumber)
	}
	if len(decoded.FlowSets) != 2 {
		t.Fatalf("expected 2 flow sets, got %d", len(decoded.FlowSets))
	}

	templateSet, ok := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	if !ok {
		t.Fatalf("expected first flow set to be TemplateFlowSet, got %T", decoded.FlowSets[0])
	}
	if templateSet.Records[0].Fields[0].Type != 8 {
		t.Fatalf("expected first field type 8 for src_addr, got %d", templateSet.Records[0].Fields[0].Type)
	}
}

func TestNFv9EncoderSequenceAdvancesPerExportPacket(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.TemplatedFlow.ObservationDomainID = 42
	enc := NewNFv9Encoder(cfg)

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}
	decodePacket := func(payload []byte) netflow.NFv9Packet {
		t.Helper()
		var decoded netflow.NFv9Packet
		if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), store, ctx, &decoded, nil); err != nil {
			t.Fatalf("decode netflow v9 payload: %v", err)
		}
		return decoded
	}

	templatePayloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"bytes"},
			BaseTemplateID: 300,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}
	if len(templatePayloads) != 1 {
		t.Fatalf("expected one template payload, got %d", len(templatePayloads))
	}
	templateDecoded := decodePacket(templatePayloads[0])
	if templateDecoded.SequenceNumber != 0 {
		t.Fatalf("expected template packet sequence 0, got %d", templateDecoded.SequenceNumber)
	}

	optionsPayloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type: "source_init",
		},
		Source: event.SourceMetadata{
			SourceID:    7,
			SourceIDSet: true,
			Sampling: &event.SamplingMetadata{
				Rate: 100,
			},
		},
	})
	if err != nil {
		t.Fatalf("source_init Encode returned error: %v", err)
	}
	if len(optionsPayloads) != 1 {
		t.Fatalf("expected one options payload, got %d", len(optionsPayloads))
	}
	optionsDecoded := decodePacket(optionsPayloads[0])
	if optionsDecoded.SequenceNumber != 1 {
		t.Fatalf("expected options packet sequence 1, got %d", optionsDecoded.SequenceNumber)
	}

	dataPayloads, err := enc.Encode(&event.Event{
		ReceivedAt: testEventTime(),
		Stream:     "flow_data",
		Fields: map[string]any{
			"bytes": uint64(64),
		},
	})
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}
	if len(dataPayloads) != 1 {
		t.Fatalf("expected one data payload, got %d", len(dataPayloads))
	}
	dataDecoded := decodePacket(dataPayloads[0])
	if dataDecoded.SequenceNumber != 2 {
		t.Fatalf("expected data packet sequence 2, got %d", dataDecoded.SequenceNumber)
	}
}

func TestNFv9EncoderBatchesCompatibleDataRecords(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
	}
	enc := NewNFv9Encoder(cfg)

	firstPayloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	}
	if len(firstPayloads) != 0 {
		t.Fatalf("expected first NetFlow v9 record to stay buffered, got %d payloads", len(firstPayloads))
	}
	second := testTemplatedFlowEvent()
	second.Fields["bytes"] = int64(654)
	secondPayloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(secondPayloads) != 1 {
		t.Fatalf("expected batched NetFlow v9 payload, got %d", len(secondPayloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(secondPayloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 payload: %v", err)
	}
	if decoded.SequenceNumber != 0 {
		t.Fatalf("expected first batched packet sequence 0, got %d", decoded.SequenceNumber)
	}
	if decoded.Count != 2 {
		t.Fatalf("expected template and data flow set count 2, got %d", decoded.Count)
	}
	if len(decoded.FlowSets) != 2 {
		t.Fatalf("expected template and data flow sets, got %d", len(decoded.FlowSets))
	}
	dataSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	if len(dataSet.Records) != 2 {
		t.Fatalf("expected two NetFlow v9 data records, got %d", len(dataSet.Records))
	}

	thirdPayloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode(third) returned error: %v", err)
	}
	if len(thirdPayloads) != 0 {
		t.Fatalf("expected third NetFlow v9 record to stay buffered, got %d payloads", len(thirdPayloads))
	}
	flushed, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(flushed) != 1 {
		t.Fatalf("expected one flushed NetFlow v9 payload, got %d", len(flushed))
	}
	var flushedDecoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(flushed[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &flushedDecoded, nil); err != nil {
		t.Fatalf("decode flushed netflow v9 payload: %v", err)
	}
	if flushedDecoded.SequenceNumber != 1 {
		t.Fatalf("expected flushed packet sequence 1, got %d", flushedDecoded.SequenceNumber)
	}
}

func TestNFv9EncoderBatchMaxBytesUsesRenderedDataFields(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 32,
		MaxBytes:   100,
	}
	enc := NewNFv9Encoder(cfg)

	for i := 0; i < 3; i++ {
		evt := testTemplatedFlowEvent()
		evt.Fields["bytes"] = int64(i)
		for j := 0; j < 128; j++ {
			evt.Fields[fmt.Sprintf("unused_%03d", j)] = "this field is not encoded into netflow v9"
		}
		payloads, err := enc.Encode(evt)
		if err != nil {
			t.Fatalf("Encode(%d) returned error: %v", i, err)
		}
		if len(payloads) != 0 {
			t.Fatalf("expected event %d to stay buffered, got %d payloads", i, len(payloads))
		}
	}
	if enc.estimatedBytes != 87 {
		t.Fatalf("expected rendered field estimate 87, got %d", enc.estimatedBytes)
	}

	payloads, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one flushed NetFlow v9 packet, got %d", len(payloads))
	}
	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 payload: %v", err)
	}
	dataSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	if len(dataSet.Records) != 3 {
		t.Fatalf("expected three NetFlow v9 data records, got %d", len(dataSet.Records))
	}
}

func TestNFv9EncoderCapacityFlushKeepsSmallTailBuffered(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.MaxDatagramBytes = 150
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 32,
		MaxBytes:   100,
	}
	enc := NewNFv9Encoder(cfg)

	var payloads [][]byte
	for i := 0; i < 4; i++ {
		evt := testTemplatedFlowEvent()
		evt.Fields["bytes"] = int64(i)
		got, err := enc.Encode(evt)
		if err != nil {
			t.Fatalf("Encode(%d) returned error: %v", i, err)
		}
		payloads = append(payloads, got...)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected capacity flush to emit one packet and keep the tail buffered, got %d payloads", len(payloads))
	}
	if len(enc.events) == 0 {
		t.Fatalf("expected small tail to remain buffered")
	}

	flushed, err := enc.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(flushed) != 1 {
		t.Fatalf("expected timer/shutdown flush to emit buffered tail, got %d payloads", len(flushed))
	}
}

func TestNFv9EncoderBatchesMultipleDataSetsInOnePacket(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
	}
	enc := NewNFv9Encoder(cfg)

	if payloads, err := enc.Encode(testTemplatedFlowEvent()); err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	} else if len(payloads) != 0 {
		t.Fatalf("expected first NetFlow v9 record to stay buffered, got %d payloads", len(payloads))
	}

	second := testTemplatedFlowEvent()
	second.Fields["template_id"] = uint32(257)
	payloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one batched NetFlow v9 payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 payload: %v", err)
	}
	if len(decoded.FlowSets) != 3 {
		t.Fatalf("expected template set and two data sets, got %d", len(decoded.FlowSets))
	}
	templateSet, ok := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	if !ok {
		t.Fatalf("expected first flow set to be TemplateFlowSet, got %T", decoded.FlowSets[0])
	}
	if len(templateSet.Records) != 2 {
		t.Fatalf("expected two template records, got %d", len(templateSet.Records))
	}
	firstSet, ok := decoded.FlowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", decoded.FlowSets[1])
	}
	secondSet, ok := decoded.FlowSets[2].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected third flow set to be DataFlowSet, got %T", decoded.FlowSets[2])
	}
	if firstSet.Id != 256 || secondSet.Id != 257 {
		t.Fatalf("expected data set ids 256 and 257, got %d and %d", firstSet.Id, secondSet.Id)
	}
}

func TestNFv9EncoderBatchedSwitchedTimesUseSharedPacketBase(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.TemplatedFlow.Data.Select = append(cfg.TemplatedFlow.Data.Select, "start_time_unix", "end_time_unix")
	cfg.Batch = config.BatchConfig{
		Enabled:    testBoolPtr(true),
		MaxRecords: 2,
	}
	enc := NewNFv9Encoder(cfg)

	if payloads, err := enc.Encode(testTemplatedFlowEvent()); err != nil {
		t.Fatalf("Encode(first) returned error: %v", err)
	} else if len(payloads) != 0 {
		t.Fatalf("expected first NetFlow v9 record to stay buffered, got %d payloads", len(payloads))
	}

	second := testTemplatedFlowEvent()
	second.Fields["start_time_unix"] = int64(1_699_999_999_900)
	second.Fields["end_time_unix"] = int64(1_700_000_000_200)
	payloads, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one batched NetFlow v9 payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 payload: %v", err)
	}
	dataSet := decoded.FlowSets[1].(netflow.DataFlowSet)
	if len(dataSet.Records) != 2 {
		t.Fatalf("expected two records, got %d", len(dataSet.Records))
	}
	decodeV9Time := func(switched uint32) int64 {
		return int64(decoded.UnixSeconds)*1000 - (int64(decoded.SystemUptime) - int64(switched))
	}
	for i, want := range []struct {
		start int64
		end   int64
	}{
		{start: 1_700_000_000_100, end: 1_700_000_000_900},
		{start: 1_699_999_999_900, end: 1_700_000_000_200},
	} {
		values := dataSet.Records[i].Values
		startRaw := values[len(values)-2].Value.([]byte)
		endRaw := values[len(values)-1].Value.([]byte)
		if got := decodeV9Time(binary.BigEndian.Uint32(startRaw)); got != want.start {
			t.Fatalf("record %d start time got %d, want %d", i, got, want.start)
		}
		if got := decodeV9Time(binary.BigEndian.Uint32(endRaw)); got != want.end {
			t.Fatalf("record %d end time got %d, want %d", i, got, want.end)
		}
	}
}

func TestNFv9EncoderUsesSwitchedTimeFields(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.TemplatedFlow.Data.Select = append(cfg.TemplatedFlow.Data.Select, "start_time_unix", "end_time_unix")
	enc := NewNFv9Encoder(cfg)

	payloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 payload: %v", err)
	}
	if decoded.UnixSeconds != 1_700_000_001 {
		t.Fatalf("expected rounded packet timestamp 1700000001, got %d", decoded.UnixSeconds)
	}
	if decoded.SystemUptime != 900 {
		t.Fatalf("expected system uptime to include packet timestamp rounding remainder, got %d", decoded.SystemUptime)
	}

	templateSet := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	fields := templateSet.Records[0].Fields
	startField := fields[len(fields)-2]
	endField := fields[len(fields)-1]
	if startField.Type != netflow.NFV9_FIELD_FIRST_SWITCHED || startField.Length != 4 {
		t.Fatalf("expected start_time_unix to use FIRST_SWITCHED/4, got type=%d length=%d", startField.Type, startField.Length)
	}
	if endField.Type != netflow.NFV9_FIELD_LAST_SWITCHED || endField.Length != 4 {
		t.Fatalf("expected end_time_unix to use LAST_SWITCHED/4, got type=%d length=%d", endField.Type, endField.Length)
	}

	dataSet := decoded.FlowSets[1].(netflow.DataFlowSet)
	values := dataSet.Records[0].Values
	startValue := values[len(values)-2]
	endValue := values[len(values)-1]
	startRaw, ok := startValue.Value.([]byte)
	if !ok {
		t.Fatalf("expected FIRST_SWITCHED value bytes, got %T", startValue.Value)
	}
	endRaw, ok := endValue.Value.([]byte)
	if !ok {
		t.Fatalf("expected LAST_SWITCHED value bytes, got %T", endValue.Value)
	}
	if got := binary.BigEndian.Uint32(startRaw); got != 0 {
		t.Fatalf("expected FIRST_SWITCHED value 0, got %d", got)
	}
	if got := binary.BigEndian.Uint32(endRaw); got != 800 {
		t.Fatalf("expected LAST_SWITCHED value 800, got %d", got)
	}
	decodeV9Time := func(switched uint32) int64 {
		return int64(decoded.UnixSeconds)*1000 - (int64(decoded.SystemUptime) - int64(switched))
	}
	if got := decodeV9Time(binary.BigEndian.Uint32(startRaw)); got != int64(1_700_000_000_100) {
		t.Fatalf("expected reconstructed start_time_unix=1700000000100, got %d", got)
	}
	if got := decodeV9Time(binary.BigEndian.Uint32(endRaw)); got != int64(1_700_000_000_900) {
		t.Fatalf("expected reconstructed end_time_unix=1700000000900, got %d", got)
	}
}

func TestNFv9EncoderSkipsIPFIXOnlyFallbackFields(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.TemplatedFlow.Data.Select = append(cfg.TemplatedFlow.Data.Select, "source_id", "header_data", "start_time_unix")
	cfg.TemplatedFlow.Data.Catalog["source_id"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_observationPointId, Length: 8, Type: "unsigned64"}
	cfg.TemplatedFlow.Data.Catalog["header_data"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_dataLinkFrameSection, Length: 0xffff, Type: "bytes"}
	enc := NewNFv9Encoder(cfg)

	evt := testTemplatedFlowEvent()
	evt.Fields["header_data"] = []byte{0xde, 0xad, 0xbe, 0xef}
	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 payload: %v", err)
	}

	templateSet := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	dataSet := decoded.FlowSets[1].(netflow.DataFlowSet)
	fields := templateSet.Records[0].Fields
	values := dataSet.Records[0].Values
	if len(fields) != len(values) {
		t.Fatalf("expected template/data widths to match, got fields=%d values=%d", len(fields), len(values))
	}
	for _, field := range fields {
		if field.Type == netflow.IPFIX_FIELD_observationPointId || field.Type == netflow.IPFIX_FIELD_dataLinkFrameSection {
			t.Fatalf("expected IPFIX-only field to be skipped, got template field %#v", field)
		}
		if !isNetFlowV9FieldID(field.Type) {
			t.Fatalf("expected only NetFlow v9 field IDs, got %#v", field)
		}
	}
	if got := fields[len(fields)-1]; got.Type != netflow.NFV9_FIELD_FIRST_SWITCHED || got.Length != 4 {
		t.Fatalf("expected supported remapped start_time_unix to remain, got %#v", got)
	}
}

func TestNFv9EncoderSkipsIPFIXOnlySchemaFields(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.TemplatedFlow.Data.Catalog["source_id"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_observationPointId, Length: 8, Type: "unsigned64"}
	cfg.TemplatedFlow.Data.Catalog["header_data"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_dataLinkFrameSection, Length: 0xffff, Type: "bytes"}
	enc := NewNFv9Encoder(cfg)

	templatePayloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"source_id", "header_data", "bytes"},
			BaseTemplateID: 300,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}
	if len(templatePayloads) != 1 {
		t.Fatalf("expected one schema template payload, got %d", len(templatePayloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}
	var templateDecoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(templatePayloads[0]), store, ctx, &templateDecoded, nil); err != nil {
		t.Fatalf("decode netflow v9 schema payload: %v", err)
	}
	templateSet := templateDecoded.FlowSets[0].(netflow.TemplateFlowSet)
	if got := templateSet.Records[0].Fields; len(got) != 1 || got[0].Type != netflow.NFV9_FIELD_IN_BYTES {
		t.Fatalf("expected only supported bytes template field, got %#v", got)
	}

	evt := testTemplatedFlowEvent()
	evt.Stream = "flow_data"
	evt.Fields["header_data"] = []byte{0xde, 0xad, 0xbe, 0xef}
	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, ctx, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 data payload: %v", err)
	}
	dataSet := decoded.FlowSets[0].(netflow.DataFlowSet)
	if got := dataSet.Records[0].Values; len(got) != 1 || got[0].Type != netflow.NFV9_FIELD_IN_BYTES {
		t.Fatalf("expected only supported bytes data value, got %#v", got)
	}
}

func TestTemplatedEncoderEncodesMacAddressFields(t *testing.T) {
	cfg := config.TemplatedFlowDataConfig{
		Select: []string{"src_mac"},
		Catalog: map[string]config.IPFIXFieldDefinition{
			"src_mac": {ID: 56, Length: 6, Type: "macAddress"},
		},
	}
	template, record, err := buildTemplatedDataRecord(cfg, map[string]any{
		"src_mac": "66:77:88:99:aa:bb",
	}, 256, false)
	if err != nil {
		t.Fatalf("buildTemplatedDataRecord returned error: %v", err)
	}
	if template.Fields[0].Type != netflow.IPFIX_FIELD_sourceMacAddress || template.Fields[0].Length != 6 {
		t.Fatalf("expected sourceMacAddress/6 template field, got %#v", template.Fields[0])
	}
	got, ok := record.Values[0].Value.([]byte)
	if !ok {
		t.Fatalf("expected encoded MAC bytes, got %T", record.Values[0].Value)
	}
	want := []byte{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb}
	if !bytes.Equal(got, want) {
		t.Fatalf("expected MAC %x, got %x", want, got)
	}
}

func TestNFv5EncoderEmitsRecord(t *testing.T) {
	enc := NewNFv5Encoder(config.EncoderConfig{Type: "netflowv5"})

	payloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(payloads))
	}

	var decoded netflowlegacy.PacketNetFlowV5
	if err := netflowlegacy.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), &decoded); err != nil {
		t.Fatalf("decode netflow v5 payload: %v", err)
	}

	if decoded.FlowSequence != 1 {
		t.Fatalf("expected flow sequence 1, got %d", decoded.FlowSequence)
	}
	if len(decoded.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(decoded.Records))
	}
	if decoded.Records[0].SrcPort != 1234 {
		t.Fatalf("expected src port 1234, got %d", decoded.Records[0].SrcPort)
	}
	if decoded.Records[0].DOctets != 321 {
		t.Fatalf("expected bytes 321, got %d", decoded.Records[0].DOctets)
	}
}

func TestSourceOptionsUseSourceSamplingMetadata(t *testing.T) {
	state := sourceOptionsFromEvent(&event.Event{
		Source: event.SourceMetadata{
			AgentIP:  "198.51.100.99",
			SourceID: 42,
			Sampling: &event.SamplingMetadata{
				Rate:       250,
				SamplePool: 54321,
				Drops:      7,
			},
		},
		Fields: map[string]any{
			"agent_ip":      "192.0.2.1",
			"source_id":     uint32(9),
			"sampling_rate": uint32(100),
			"sample_pool":   uint32(12345),
			"drops":         uint32(3),
		},
		Payload: event.SourceInit{
			AgentIP:      "192.0.2.2",
			SourceID:     10,
			SamplingRate: 101,
			SamplePool:   12346,
			Drops:        4,
		},
	})

	if state.agentIP != "198.51.100.99" {
		t.Fatalf("expected source agent_ip 198.51.100.99, got %q", state.agentIP)
	}
	if state.sourceID != 42 {
		t.Fatalf("expected source_id 42, got %d", state.sourceID)
	}
	if state.samplingRate != 250 {
		t.Fatalf("expected source sampling_rate 250, got %d", state.samplingRate)
	}
	if state.samplePool != 54321 {
		t.Fatalf("expected source sample_pool 54321, got %d", state.samplePool)
	}
	if state.drops != 7 {
		t.Fatalf("expected source drops 7, got %d", state.drops)
	}
}

func TestIPFIXSchemaDataUsesSourceSamplingMetadata(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.Data.Catalog["sampling_rate"] = config.IPFIXFieldDefinition{ID: 34, Length: 4, Type: "unsigned32"}
	cfg.TemplatedFlow.Data.Catalog["sample_pool"] = config.IPFIXFieldDefinition{ID: 310, Length: 4, Type: "unsigned32"}
	cfg.TemplatedFlow.Data.Catalog["drops"] = config.IPFIXFieldDefinition{ID: 133, Length: 8, Type: "unsigned64"}
	cfg.TemplatedFlow.Data.Catalog["source_id"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_observationPointId, Length: 8, Type: "unsigned64"}
	enc := NewIPFIXEncoder(cfg)

	if _, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"source_id", "sampling_rate", "sample_pool", "drops"},
			BaseTemplateID: 300,
		},
	}); err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}

	evt := &event.Event{
		ReceivedAt: testEventTime(),
		Stream:     "flow_data",
		Source: event.SourceMetadata{
			SourceID: 42,
			Sampling: &event.SamplingMetadata{
				Rate:       250,
				SamplePool: 54321,
				Drops:      7,
			},
		},
		Fields: map[string]any{
			"bytes":         uint64(64),
			"sampling_rate": uint32(100),
			"sample_pool":   uint32(12345),
			"drops":         uint32(3),
		},
	}
	fields := eventFieldsWithMetadataForSchema(evt, enc.dataSchemas["flow_data"].fields)
	if fields["source_id"] != uint32(42) || fields["sampling_rate"] != uint32(250) || fields["sample_pool"] != uint32(54321) || fields["drops"] != uint32(7) {
		t.Fatalf("expected source sampling metadata to materialize for schema, got %#v", fields)
	}
	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one data payload, got %d", len(payloads))
	}
	record := enc.dataSchemas["flow_data"].templateForFields(nil)
	if record.Fields[0].Type != netflow.IPFIX_FIELD_observationPointId || record.Fields[1].Type != 34 || record.Fields[2].Type != 310 || record.Fields[3].Type != 133 {
		t.Fatalf("unexpected metadata template fields: %#v", record.Fields)
	}
}

func TestIPFIXSchemaDataUsesAgentIPv6Metadata(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.Data.Catalog["agent_ip"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_exporterIPv4Address, Length: 4, Type: "ipv4Address"}
	cfg.TemplatedFlow.Data.Catalog["agent_ipv6"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_exporterIPv6Address, Length: 16, Type: "ipv6Address"}
	enc := NewIPFIXEncoder(cfg)

	if _, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"agent_ip", "agent_ipv6"},
			BaseTemplateID: 300,
		},
	}); err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}

	evt := &event.Event{
		ReceivedAt: testEventTime(),
		Stream:     "flow_data",
		Source: event.SourceMetadata{
			AgentIP: "2001:db8::99",
		},
		Fields: map[string]any{
			"bytes": uint64(64),
		},
	}
	fields := eventFieldsWithMetadataForSchema(evt, enc.dataSchemas["flow_data"].fields)
	if _, ok := fields["agent_ip"]; ok {
		t.Fatalf("expected IPv6 metadata not to materialize as agent_ip, got %#v", fields)
	}
	if fields["agent_ipv6"] != "2001:db8::99" {
		t.Fatalf("expected agent_ipv6 metadata, got %#v", fields)
	}
	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one data payload, got %d", len(payloads))
	}
	record := enc.dataSchemas["flow_data"].templateForFields(nil)
	if record.Fields[0].Type != netflow.IPFIX_FIELD_exporterIPv4Address || record.Fields[1].Type != netflow.IPFIX_FIELD_exporterIPv6Address {
		t.Fatalf("unexpected agent address template fields: %#v", record.Fields)
	}
}

func TestNFv9EncoderPassesThroughOptionsTemplate(t *testing.T) {
	enc := NewNFv9Encoder(config.EncoderConfig{Type: "netflowv9"})
	evt := &event.Event{
		Fields: map[string]any{
			"source_id": uint32(9),
		},
		Payload: netflow.NFv9OptionsTemplateRecord{
			TemplateId:   300,
			ScopeLength:  4,
			OptionLength: 4,
			Scopes:       []netflow.Field{{Type: 1, Length: 4}},
			Options:      []netflow.Field{{Type: 34, Length: 4}},
		},
	}

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, &decoded, nil); err != nil {
		t.Fatalf("decode netflow v9 payload: %v", err)
	}

	if _, ok := decoded.FlowSets[0].(netflow.NFv9OptionsTemplateFlowSet); !ok {
		t.Fatalf("expected options template flow set, got %T", decoded.FlowSets[0])
	}
}

func TestNFv9AggregationSchemaOptionsDataUsesScopeIDs(t *testing.T) {
	cfg := testTFlowEncoderConfig("netflowv9")
	cfg.TemplatedFlow.ObservationDomainID = 888
	cfg.TemplatedFlow.Data.Catalog["observation_domain_id"] = config.IPFIXFieldDefinition{ID: 149, Length: 4, Type: "unsigned32"}
	cfg.TemplatedFlow.Data.Catalog["if_index"] = config.IPFIXFieldDefinition{ID: 10, Length: 4, Type: "unsigned32"}
	cfg.TemplatedFlow.Data.Catalog["if_name"] = config.IPFIXFieldDefinition{ID: 82, Length: 0xffff, Type: "string"}
	enc := NewNFv9Encoder(cfg)

	schemaPayloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "interface_options",
		},
		Payload: event.AggregationSchema{
			Stream: "interface_options",
			Fields: []event.SchemaField{
				{Role: "static", Name: "tflow_record_type", Value: "options"},
				{Role: "key", Name: "observation_domain_id"},
				{Role: "key", Name: "if_index"},
				{Role: "current", Name: "if_name"},
			},
			BaseTemplateID: 1300,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}
	if len(schemaPayloads) != 1 {
		t.Fatalf("expected one options schema payload, got %d", len(schemaPayloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}
	var schemaDecoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(schemaPayloads[0]), store, ctx, &schemaDecoded, nil); err != nil {
		t.Fatalf("decode options schema payload: %v", err)
	}
	optionsTemplate, ok := schemaDecoded.FlowSets[0].(netflow.NFv9OptionsTemplateFlowSet)
	if !ok {
		t.Fatalf("expected options template flow set, got %T", schemaDecoded.FlowSets[0])
	}
	record := optionsTemplate.Records[0]
	if record.TemplateId != 1300 || record.ScopeLength != 8 || record.OptionLength != 4 {
		t.Fatalf("unexpected options template lengths: %#v", record)
	}
	if record.Scopes[0].Type != 1 || record.Scopes[1].Type != 2 || record.Options[0].Type != 82 {
		t.Fatalf("unexpected options template fields: scopes=%#v options=%#v", record.Scopes, record.Options)
	}

	dataPayloads, err := enc.Encode(&event.Event{
		ReceivedAt: testEventTime(),
		Stream:     "interface_options",
		Fields: map[string]any{
			"observation_domain_id": uint32(777),
			"if_index":              uint32(2),
			"if_name":               "eth0",
		},
	})
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}
	if len(dataPayloads) != 1 {
		t.Fatalf("expected one options data payload, got %d", len(dataPayloads))
	}
	var dataDecoded netflow.NFv9Packet
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(dataPayloads[0]), store, ctx, &dataDecoded, nil); err != nil {
		t.Fatalf("decode options data payload: %v", err)
	}
	optionsData, ok := dataDecoded.FlowSets[0].(netflow.OptionsDataFlowSet)
	if !ok {
		t.Fatalf("expected options data flow set, got %T", dataDecoded.FlowSets[0])
	}
	values := optionsData.Records[0]
	if !bytes.Equal(values.ScopesValues[0].Value.([]byte), encodeU32(777)) || !bytes.Equal(values.ScopesValues[1].Value.([]byte), encodeU32(2)) {
		t.Fatalf("unexpected options scope values: %#v", values.ScopesValues)
	}
	if !bytes.Equal(values.OptionsValues[0].Value.([]byte), []byte("eth0")) {
		t.Fatalf("expected interface name option eth0, got %#v", values.OptionsValues[0].Value)
	}
}

func TestIPFIXTemplatePacketDoesNotAdvanceSequence(t *testing.T) {
	enc := NewIPFIXEncoder(testTFlowEncoderConfig("ipfix"))

	templatePayloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"source_id": uint32(42),
		},
		Payload: netflow.TemplateRecord{
			TemplateId: 256,
			FieldCount: 1,
			Fields:     []netflow.Field{{Type: netflow.IPFIX_FIELD_octetDeltaCount, Length: 8}},
		},
	})
	if err != nil {
		t.Fatalf("template Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var templateDecoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(templatePayloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &templateDecoded); err != nil {
		t.Fatalf("decode ipfix template payload: %v", err)
	}
	if templateDecoded.SequenceNumber != 0 {
		t.Fatalf("expected template sequence 0, got %d", templateDecoded.SequenceNumber)
	}

	dataPayloads, err := enc.Encode(testTemplatedFlowEvent())
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}

	var dataDecoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(dataPayloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &dataDecoded); err != nil {
		t.Fatalf("decode ipfix data payload: %v", err)
	}
	if dataDecoded.SequenceNumber != 0 {
		t.Fatalf("expected first data packet sequence 0 after template set, got %d", dataDecoded.SequenceNumber)
	}
}

func TestIPFIXSchemaDrivenDataRecordKeepsTemplateWidth(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.ObservationDomainID = 42
	enc := NewIPFIXEncoder(cfg)

	_, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"src_addr", "dst_addr", "src_port", "dst_port", "proto", "bytes", "packets", "start_time_unix", "end_time_unix"},
			BaseTemplateID: 256,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}

	evt := testTemplatedFlowEvent()
	delete(evt.Fields, "end_time_unix")
	evt.Fields["observation_domain_id"] = uint32(42)

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}

	// Prime the decode-side template store with the same schema packet.
	templatePayloads, err := enc.encodeSchemaTemplates(enc.dataSchemas["flow_data"])
	if err != nil {
		t.Fatalf("encodeSchemaTemplates returned error: %v", err)
	}
	for _, payload := range templatePayloads {
		var templatePacket netflow.IPFIXPacket
		if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), store, ctx, nil, &templatePacket); err != nil {
			t.Fatalf("decode schema template payload: %v", err)
		}
	}

	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, ctx, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix data payload: %v", err)
	}
	dataSet := decoded.FlowSets[0].(netflow.DataFlowSet)
	if len(dataSet.Records[0].Values) != 9 {
		t.Fatalf("expected 9 values in data record, got %d", len(dataSet.Records[0].Values))
	}
}

func TestIPFIXSchemaDrivenVariableLengthDataIsLengthPrefixed(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.Data.Catalog["header_data"] = config.IPFIXFieldDefinition{ID: 315, Length: 0xffff, Type: "bytes"}
	enc := NewIPFIXEncoder(cfg)

	_, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"header_data", "bytes"},
			BaseTemplateID: 256,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}

	headerData := append([]byte{0x01}, bytes.Repeat([]byte{0xab}, 299)...)
	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: testEventTime(),
		Stream:     "flow_data",
		Fields: map[string]any{
			"header_data": headerData,
			"bytes":       uint64(300),
		},
	})
	if err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one data payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	ctx := netflow.FlowContext{RouterKey: "test-router"}
	templatePayloads, err := enc.encodeSchemaTemplates(enc.dataSchemas["flow_data"])
	if err != nil {
		t.Fatalf("encodeSchemaTemplates returned error: %v", err)
	}
	for _, payload := range templatePayloads {
		var templatePacket netflow.IPFIXPacket
		if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), store, ctx, nil, &templatePacket); err != nil {
			t.Fatalf("decode schema template payload: %v", err)
		}
	}

	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, ctx, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix data payload: %v", err)
	}
	dataSet := decoded.FlowSets[0].(netflow.DataFlowSet)
	if len(dataSet.Records) != 1 {
		t.Fatalf("expected one data record, got %d", len(dataSet.Records))
	}
	values := dataSet.Records[0].Values
	if len(values) != 2 {
		t.Fatalf("expected two values, got %d", len(values))
	}
	if got := values[0].Value.([]byte); !bytes.Equal(got, headerData) {
		t.Fatalf("expected decoded header_data length %d, got %d", len(headerData), len(got))
	}
	if got := values[1].Value.([]byte); !bytes.Equal(got, []byte{0, 0, 0, 0, 0, 0, 0x01, 0x2c}) {
		t.Fatalf("expected decoded bytes value 300, got %v", got)
	}
}

func TestIPFIXSchemaDrivenLayerAddressFieldsUseIndependentFamilies(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.Data.Catalog["ip_1_src_addr"] = config.IPFIXFieldDefinition{ID: 8, Length: 4, Type: "ipv4Address"}
	cfg.TemplatedFlow.Data.Catalog["ip_1_dst_addr"] = config.IPFIXFieldDefinition{ID: 12, Length: 4, Type: "ipv4Address"}
	cfg.TemplatedFlow.Data.Catalog["ip_2_src_addr"] = config.IPFIXFieldDefinition{ID: 8, Length: 4, Type: "ipv4Address"}
	cfg.TemplatedFlow.Data.Catalog["ip_2_dst_addr"] = config.IPFIXFieldDefinition{ID: 12, Length: 4, Type: "ipv4Address"}
	enc := NewIPFIXEncoder(cfg)

	templatePayloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"ip_1_src_addr", "ip_1_dst_addr", "ip_2_src_addr", "ip_2_dst_addr"},
			BaseTemplateID: 300,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}
	if len(templatePayloads) != 4 {
		t.Fatalf("expected four address-family template variants, got %d", len(templatePayloads))
	}

	evt := &event.Event{
		ReceivedAt: testEventTime(),
		Fields: map[string]any{
			"ip_1_src_addr": "203.0.113.1",
			"ip_1_dst_addr": "203.0.113.2",
			"ip_2_src_addr": "2001:db8::1",
			"ip_2_dst_addr": "2001:db8::2",
		},
	}
	template := enc.dataSchemas["flow_data"].templateForFields(evt.Fields)
	if template.TemplateId != 302 {
		t.Fatalf("expected mixed-family template id 302, got %d", template.TemplateId)
	}
	if template.Fields[0].Type != netflow.IPFIX_FIELD_sourceIPv4Address || template.Fields[1].Type != netflow.IPFIX_FIELD_destinationIPv4Address {
		t.Fatalf("expected first IP layer to use IPv4 fields, got %#v", template.Fields[:2])
	}
	if template.Fields[2].Type != netflow.IPFIX_FIELD_sourceIPv6Address || template.Fields[3].Type != netflow.IPFIX_FIELD_destinationIPv6Address {
		t.Fatalf("expected second IP layer to use IPv6 fields, got %#v", template.Fields[2:])
	}
	if _, err := enc.Encode(evt); err != nil {
		t.Fatalf("data Encode returned error: %v", err)
	}
}

func TestIPFIXSchemaDrivenFixedFamilyEmitsSingleTemplate(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	enc := NewIPFIXEncoder(cfg)

	ipv4Templates, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data_ipv4",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data_ipv4",
			FieldNames:     []string{"src_addr", "dst_addr", "bytes"},
			Match:          map[string]string{"ip_family": "ipv4"},
			BaseTemplateID: 256,
		},
	})
	if err != nil {
		t.Fatalf("ipv4 schema Encode returned error: %v", err)
	}
	if len(ipv4Templates) != 1 {
		t.Fatalf("expected one ipv4 template, got %d", len(ipv4Templates))
	}

	ipv6Templates, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data_ipv6",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data_ipv6",
			FieldNames:     []string{"src_addr", "dst_addr", "bytes"},
			Match:          map[string]string{"ip_family": "ipv6"},
			BaseTemplateID: 258,
		},
	})
	if err != nil {
		t.Fatalf("ipv6 schema Encode returned error: %v", err)
	}
	if len(ipv6Templates) != 1 {
		t.Fatalf("expected one ipv6 template, got %d", len(ipv6Templates))
	}

	if template := enc.dataSchemas["flow_data_ipv4"].templates()[0]; template.TemplateId != 256 {
		t.Fatalf("expected ipv4 template id 256, got %d", template.TemplateId)
	}
	if template := enc.dataSchemas["flow_data_ipv6"].templates()[0]; template.TemplateId != 258 {
		t.Fatalf("expected ipv6 template id 258, got %d", template.TemplateId)
	}
}

func TestIPFIXSchemaDrivenNATVariantsPairPostNATWithPrimaryFamily(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.Data.Catalog["nat_src_addr"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_postNATSourceIPv4Address, Length: 4, Type: "ipv4Address"}
	cfg.TemplatedFlow.Data.Catalog["nat_dst_addr"] = config.IPFIXFieldDefinition{ID: netflow.IPFIX_FIELD_postNATDestinationIPv4Address, Length: 4, Type: "ipv4Address"}
	enc := NewIPFIXEncoder(cfg)

	payloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"src_addr", "dst_addr", "nat_src_addr", "nat_dst_addr"},
			BaseTemplateID: 256,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}
	if len(payloads) != 4 {
		t.Fatalf("expected four NAT address-family template variants, got %d", len(payloads))
	}

	templates := enc.dataSchemas["flow_data"].templates()
	for i, template := range templates {
		if want := uint16(256 + i); template.TemplateId != want {
			t.Fatalf("expected template %d to have id %d, got %d", i, want, template.TemplateId)
		}
	}

	firstIPv6 := templates[1]
	if firstIPv6.Fields[0].Type != netflow.IPFIX_FIELD_sourceIPv6Address || firstIPv6.Fields[1].Type != netflow.IPFIX_FIELD_destinationIPv6Address {
		t.Fatalf("expected template 257 to use IPv6 source/destination fields, got %#v", firstIPv6.Fields[:2])
	}
	if firstIPv6.Fields[2].Type != netflow.IPFIX_FIELD_postNATSourceIPv6Address || firstIPv6.Fields[3].Type != netflow.IPFIX_FIELD_postNATDestinationIPv6Address {
		t.Fatalf("expected template 257 to use IPv6 post-NAT fields, got %#v", firstIPv6.Fields[2:])
	}
}

func TestIPFIXDefaultMPLSStackSectionUsesThreeByteField(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.Data.Catalog["mpls_label_stack_section_1"] = config.IPFIXFieldDefinition{ID: 70, Length: 3, Type: "bytes"}
	def := cfg.TemplatedFlow.Data.Catalog["mpls_label_stack_section_1"]
	if def.ID != 70 || def.Length != 3 || def.Type != "bytes" {
		t.Fatalf("expected MPLS stack section IE 70 length 3 bytes, got %#v", def)
	}

	template, record, err := buildTemplatedDataRecordWithNames(
		cfg.TemplatedFlow.Data,
		map[string]any{"mpls_label_stack_section_1": []byte{0x00, 0x01, 0x31}},
		[]string{"mpls_label_stack_section_1"},
		256,
		templatedEncodingContext{},
	)
	if err != nil {
		t.Fatalf("buildTemplatedDataRecordWithNames returned error: %v", err)
	}
	if template.Fields[0].Type != 70 || template.Fields[0].Length != 3 {
		t.Fatalf("expected template field IE 70 length 3, got %#v", template.Fields[0])
	}
	if got, ok := record.Values[0].Value.([]byte); !ok || !bytes.Equal(got, []byte{0x00, 0x01, 0x31}) {
		t.Fatalf("expected three MPLS stack-section bytes, got %#v", got)
	}
}

func TestIPFIXSchemaFieldsUseEncoderCatalogForEnterpriseMapping(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.TemplatedFlow.ObservationDomainID = 42
	cfg.TemplatedFlow.Data.Catalog["tenant_id"] = config.IPFIXFieldDefinition{
		Name:             "tenantId",
		ID:               12345,
		PEN:              32473,
		Length:           4,
		Type:             "unsigned32",
		EnterpriseScoped: true,
	}
	enc := NewIPFIXEncoder(cfg)

	payloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream: "flow_data",
			Fields: []event.SchemaField{
				{Role: "current", Name: "tenant_id"},
			},
			FieldNames:     []string{"tenant_id"},
			BaseTemplateID: 300,
		},
	})
	if err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one schema payload, got %d", len(payloads))
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode schema payload: %v", err)
	}
	templateSet := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	field := templateSet.Records[0].Fields[0]
	if !field.PenProvided || field.Pen != 32473 || field.Type != 12345 || field.Length != 4 {
		t.Fatalf("expected enterprise field 12345/32473 length 4, got %#v", field)
	}
}

func TestIPFIXEncoderUsesIPv6InformationElementsForIPv6Addresses(t *testing.T) {
	enc := NewIPFIXEncoder(testTFlowEncoderConfig("ipfix"))
	evt := testTemplatedFlowEvent()
	evt.Fields["src_addr"] = "2001:db8::10"
	evt.Fields["dst_addr"] = "2001:db8::20"

	payloads, err := enc.Encode(evt)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	store := templates.NewTemplateFlowStore()
	store.Start()
	var decoded netflow.IPFIXPacket
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payloads[0]), store, netflow.FlowContext{RouterKey: "test-router"}, nil, &decoded); err != nil {
		t.Fatalf("decode ipfix payload: %v", err)
	}

	templateSet := decoded.FlowSets[0].(netflow.TemplateFlowSet)
	if templateSet.Records[0].Fields[0].Type != netflow.IPFIX_FIELD_sourceIPv6Address {
		t.Fatalf("expected IPv6 src IE, got %d", templateSet.Records[0].Fields[0].Type)
	}
	if templateSet.Records[0].Fields[1].Type != netflow.IPFIX_FIELD_destinationIPv6Address {
		t.Fatalf("expected IPv6 dst IE, got %d", templateSet.Records[0].Fields[1].Type)
	}
}

func TestEncodeIPFIXBooleanValue(t *testing.T) {
	def := config.IPFIXFieldDefinition{ID: 388, Length: 1, Type: "boolean"}
	encoded, err := encodeIPFIXValue(def, true)
	if err != nil {
		t.Fatalf("encodeIPFIXValue returned error: %v", err)
	}
	if !bytes.Equal(encoded, []byte{1}) {
		t.Fatalf("expected true boolean to encode as 1, got %x", encoded)
	}

	encoded, err = encodeIPFIXValue(def, false)
	if err != nil {
		t.Fatalf("encodeIPFIXValue returned error: %v", err)
	}
	if !bytes.Equal(encoded, []byte{0}) {
		t.Fatalf("expected false boolean to encode as 0, got %x", encoded)
	}
}

func TestAggregatorDropsPacketsMissingConfiguredKeys(t *testing.T) {
	agg, err := aggregate.New(config.AggregatorConfig{
		KeyFields: []string{"src_addr", "dst_addr"},
	})
	if err != nil {
		t.Fatalf("New aggregator returned error: %v", err)
	}

	events, err := agg.Process(&event.Event{
		Fields: map[string]any{
			"bytes": int64(64),
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected packet without aggregation keys to be dropped, got %d events", len(events))
	}
}

func testSFlowEvent(agentIP string) *event.Event {
	return &event.Event{
		Fields: map[string]any{
			"agent_ip":        "192.0.2.1",
			"protocol":        uint32(1),
			"frame_length":    uint32(60),
			"original_length": uint32(60),
			"header_data":     []byte{0, 1, 2, 3},
		},
		SFlow: &event.SFlowMetadata{
			AgentIP: agentIP,
		},
	}
}

func decodeSFlowPacket(t *testing.T, payload []byte) *sflow.Packet {
	t.Helper()
	packet := &sflow.Packet{}
	if err := sflow.DecodeMessageVersion(bytes.NewBuffer(payload), packet); err != nil {
		t.Fatalf("decode sflow payload: %v", err)
	}
	return packet
}

func decodeDelimitedFlowMessage(t *testing.T, payload []byte) *flowpb.FlowMessage {
	t.Helper()
	size, n := protowire.ConsumeVarint(payload)
	if n < 0 {
		t.Fatalf("failed to decode protobuf length prefix")
	}
	msg := &flowpb.FlowMessage{}
	if err := proto.Unmarshal(payload[n:n+int(size)], msg); err != nil {
		t.Fatalf("unmarshal flow message: %v", err)
	}
	return msg
}

func decodeFlowMessage(t *testing.T, payload []byte) *flowpb.FlowMessage {
	t.Helper()
	msg := &flowpb.FlowMessage{}
	if err := proto.Unmarshal(payload, msg); err != nil {
		t.Fatalf("unmarshal flow message: %v", err)
	}
	return msg
}

func testBoolPtr(v bool) *bool {
	return &v
}

func assertFlowDirectionTemplateAndValue(t *testing.T, flowSets []interface{}, fieldType uint16) {
	t.Helper()
	if len(flowSets) != 2 {
		t.Fatalf("expected template and data flow sets, got %d", len(flowSets))
	}
	templateSet, ok := flowSets[0].(netflow.TemplateFlowSet)
	if !ok {
		t.Fatalf("expected first flow set to be TemplateFlowSet, got %T", flowSets[0])
	}
	if len(templateSet.Records) != 1 || len(templateSet.Records[0].Fields) != 1 {
		t.Fatalf("expected one-field template, got %#v", templateSet.Records)
	}
	field := templateSet.Records[0].Fields[0]
	if field.Type != fieldType || field.Length != 1 {
		t.Fatalf("expected flow direction field type=%d length=1, got type=%d length=%d", fieldType, field.Type, field.Length)
	}
	dataSet, ok := flowSets[1].(netflow.DataFlowSet)
	if !ok {
		t.Fatalf("expected second flow set to be DataFlowSet, got %T", flowSets[1])
	}
	if len(dataSet.Records) < 1 || len(dataSet.Records[0].Values) != 1 {
		t.Fatalf("expected first flow direction value, got %#v", dataSet.Records)
	}
	if got := dataSet.Records[0].Values[0].Value.([]byte); !bytes.Equal(got, []byte{1}) {
		t.Fatalf("expected flow direction value 1, got %v", got)
	}
}

func testTFlowEncoderConfig(typ string) config.EncoderConfig {
	return config.EncoderConfig{
		Type: typ,
		TemplatedFlow: config.TemplatedFlowConfig{
			OptionsTemplateBaseID: 1024,
			Data: config.TemplatedFlowDataConfig{
				Select: []string{"src_addr", "dst_addr", "src_port", "dst_port", "proto", "bytes", "packets"},
				Catalog: map[string]config.IPFIXFieldDefinition{
					"src_addr":        {ID: 8, Length: 4, Type: "ipv4Address"},
					"dst_addr":        {ID: 12, Length: 4, Type: "ipv4Address"},
					"src_port":        {ID: 7, Length: 2, Type: "unsigned16"},
					"dst_port":        {ID: 11, Length: 2, Type: "unsigned16"},
					"proto":           {ID: 4, Length: 1, Type: "unsigned8"},
					"bytes":           {ID: 1, Length: 8, Type: "unsigned64"},
					"packets":         {ID: 2, Length: 8, Type: "unsigned64"},
					"start_time_unix": {ID: 152, Length: 8, Type: "unsigned64"},
					"end_time_unix":   {ID: 153, Length: 8, Type: "unsigned64"},
				},
			},
		},
	}
}

func testTemplatedFlowEvent() *event.Event {
	return &event.Event{
		ReceivedAt: testEventTime(),
		Fields: map[string]any{
			"source_id":       uint32(42),
			"src_addr":        "192.0.2.10",
			"dst_addr":        "192.0.2.20",
			"src_port":        uint32(1234),
			"dst_port":        uint32(4321),
			"proto":           uint32(17),
			"bytes":           int64(321),
			"packets":         int64(7),
			"start_time_unix": int64(1_700_000_000_100),
			"end_time_unix":   int64(1_700_000_000_900),
		},
	}
}

func testEventTime() time.Time {
	return time.Unix(1_700_000_001, 0).UTC()
}
