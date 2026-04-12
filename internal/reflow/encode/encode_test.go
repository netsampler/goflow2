package encode

import (
	"bytes"
	"net/netip"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

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
			Enabled: true,
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
		AllowTruncate:    true,
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

func TestSFlowEncoderPacketSequenceAdvancesPerDatagram(t *testing.T) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
		Batch: config.BatchConfig{
			Enabled:    true,
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
