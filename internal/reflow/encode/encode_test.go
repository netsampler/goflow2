package encode

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/internal/reflow/aggregate"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
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

func TestIPFIXEncoderEmitsTemplateAndDataRecord(t *testing.T) {
	enc := NewIPFIXEncoder(testTFlowEncoderConfig("ipfix"))

	payloads, err := enc.Encode(testTemplatedFlowEvent())
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

func TestIPFIXEncoderUsesEventObservationDomainID(t *testing.T) {
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
	if decoded.ObservationDomainId != 777 {
		t.Fatalf("expected observation domain 777, got %d", decoded.ObservationDomainId)
	}
}

func TestIPFIXEncoderConfigObservationDomainIDOverridesEvent(t *testing.T) {
	cfg := testTFlowEncoderConfig("ipfix")
	cfg.ObservationDomainID = 888
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

func TestNFv9EncoderEmitsTemplateAndDataRecord(t *testing.T) {
	enc := NewNFv9Encoder(testTFlowEncoderConfig("netflowv9"))

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

func TestAggregatorDropsPacketsMissingConfiguredKeys(t *testing.T) {
	agg, err := aggregate.New(config.AggregatorConfig{
		Enabled:   true,
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

func testTFlowEncoderConfig(typ string) config.EncoderConfig {
	return config.EncoderConfig{
		Type: typ,
		TFlowData: config.TFlowDataConfig{
			Select: []string{"src_addr", "dst_addr", "src_port", "dst_port", "proto", "bytes", "packets"},
			Catalog: map[string]config.IPFIXFieldDefinition{
				"src_addr": {ID: 8, NetFlowV9ID: 8, Length: 4, Type: "ipv4Address"},
				"dst_addr": {ID: 12, NetFlowV9ID: 12, Length: 4, Type: "ipv4Address"},
				"src_port": {ID: 7, NetFlowV9ID: 7, Length: 2, Type: "unsigned16"},
				"dst_port": {ID: 11, NetFlowV9ID: 11, Length: 2, Type: "unsigned16"},
				"proto":    {ID: 4, NetFlowV9ID: 4, Length: 1, Type: "unsigned8"},
				"bytes":    {ID: 1, NetFlowV9ID: 1, Length: 8, Type: "unsigned64"},
				"packets":  {ID: 2, NetFlowV9ID: 2, Length: 8, Type: "unsigned64"},
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
