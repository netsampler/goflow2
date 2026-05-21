package encode

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/internal/reflow/aggregate"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/processor"
	flowpb "github.com/netsampler/goflow2/v3/pb"
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
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "cmd", "reflow", "nested-ip-layers.json"))
	if err != nil {
		t.Fatalf("read nested-ip-layers.json: %v", err)
	}
	proc := processor.NewBuiltin(config.ProcessorConfig{})
	events, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{
			Type: "json",
			JSON: event.JSONMetadata{Flavor: "reflow"},
		},
		Message: raw,
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

func TestIPFIXSourceOptionsUseExporterObservationDomain(t *testing.T) {
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
	optionsData, ok := decoded.FlowSets[1].(netflow.OptionsDataFlowSet)
	if !ok {
		t.Fatalf("expected options data flow set, got %T", decoded.FlowSets[1])
	}
	scope := optionsData.Records[0].ScopesValues[0]
	if scope.Type != netflow.IPFIX_FIELD_observationDomainId {
		t.Fatalf("expected observationDomainId scope, got %d", scope.Type)
	}
	if got := scope.Value.([]byte); !bytes.Equal(got, encodeU32(888)) {
		t.Fatalf("expected options observation domain scope 888, got %v", got)
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
		t.Fatalf("expected first data packet after options sequence 1, got %d", dataDecoded.SequenceNumber)
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
	enc := NewIPFIXEncoder(cfg)

	if _, err := enc.Encode(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:         "flow_data",
			FieldNames:     []string{"sampling_rate", "sample_pool", "drops"},
			BaseTemplateID: 300,
		},
	}); err != nil {
		t.Fatalf("schema Encode returned error: %v", err)
	}

	evt := &event.Event{
		ReceivedAt: testEventTime(),
		Stream:     "flow_data",
		Source: event.SourceMetadata{
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
	if fields["sampling_rate"] != uint32(250) || fields["sample_pool"] != uint32(54321) || fields["drops"] != uint32(7) {
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
	if record.Fields[0].Type != 34 || record.Fields[1].Type != 310 || record.Fields[2].Type != 133 {
		t.Fatalf("unexpected metadata template fields: %#v", record.Fields)
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
		t.Fatalf("expected first data packet sequence 0, got %d", dataDecoded.SequenceNumber)
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
