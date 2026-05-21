package protoproducer

import (
	"bytes"
	"net/netip"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/format"
	_ "github.com/netsampler/goflow2/v3/format/binary"
	_ "github.com/netsampler/goflow2/v3/format/json"
	"github.com/netsampler/goflow2/v3/producer"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

func BenchmarkOriginalIPFIXToFormat(b *testing.B) {
	cfg, err := (&ProducerConfig{}).Compile()
	if err != nil {
		b.Fatalf("Compile returned error: %v", err)
	}
	prod, err := CreateProtoProducer(cfg, nil)
	if err != nil {
		b.Fatalf("CreateProtoProducer returned error: %v", err)
	}
	defer prod.Close()

	args := &producer.ProduceArgs{
		Src:            netip.MustParseAddrPort("192.0.2.1:4739"),
		Dst:            netip.MustParseAddrPort("192.0.2.2:4739"),
		SamplerAddress: netip.MustParseAddr("192.0.2.1"),
		TimeReceived:   time.Unix(1_700_000_001, 0).UTC(),
		FlowContext: &netflow.FlowContext{
			RouterKey: "192.0.2.1",
		},
	}
	packet := benchOriginalIPFIXPacket()

	benchmarks := []struct {
		name   string
		format string
	}{
		{name: "protobuf_payload_for_kafka", format: "bin"},
		{name: "json", format: "json"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			formatter, err := format.FindFormat(bm.format)
			if err != nil {
				b.Fatalf("FindFormat returned error: %v", err)
			}
			var totalBytes int
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				messages, err := prod.Produce(packet, args)
				if err != nil {
					b.Fatalf("Produce returned error: %v", err)
				}
				if len(messages) != 1 {
					b.Fatalf("expected 1 message, got %d", len(messages))
				}
				_, payload, err := formatter.Format(messages[0])
				if err != nil {
					b.Fatalf("Format returned error: %v", err)
				}
				totalBytes += len(payload)
				prod.Commit(messages)
			}
			if totalBytes == 0 {
				b.Fatalf("expected formatted payload bytes")
			}
		})
	}
}

func BenchmarkOriginalIPFIXProduceOnly(b *testing.B) {
	cfg, err := (&ProducerConfig{}).Compile()
	if err != nil {
		b.Fatalf("Compile returned error: %v", err)
	}
	prod, err := CreateProtoProducer(cfg, nil)
	if err != nil {
		b.Fatalf("CreateProtoProducer returned error: %v", err)
	}
	defer prod.Close()

	args := &producer.ProduceArgs{
		Src:            netip.MustParseAddrPort("192.0.2.1:4739"),
		SamplerAddress: netip.MustParseAddr("192.0.2.1"),
		TimeReceived:   time.Unix(1_700_000_001, 0).UTC(),
		FlowContext:    &netflow.FlowContext{RouterKey: "192.0.2.1"},
	}
	packet := benchOriginalIPFIXPacket()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		messages, err := prod.Produce(packet, args)
		if err != nil {
			b.Fatalf("Produce returned error: %v", err)
		}
		if len(messages) != 1 {
			b.Fatalf("expected 1 message, got %d", len(messages))
		}
		prod.Commit(messages)
	}
}

func BenchmarkOriginalRawIPFIXToFormat(b *testing.B) {
	cfg, err := (&ProducerConfig{}).Compile()
	if err != nil {
		b.Fatalf("Compile returned error: %v", err)
	}
	prod, err := CreateProtoProducer(cfg, nil)
	if err != nil {
		b.Fatalf("CreateProtoProducer returned error: %v", err)
	}
	defer prod.Close()

	ctx := netflow.FlowContext{RouterKey: "192.0.2.1"}
	store := templates.NewTemplateFlowStore()
	store.Start()
	defer store.Close()
	templatePayload, dataPayload := benchOriginalIPFIXPayloads(b)
	if err := netflow.DecodeMessageVersion(bytes.NewBuffer(templatePayload), store, ctx, nil, &netflow.IPFIXPacket{}); err != nil {
		b.Fatalf("decode template payload: %v", err)
	}

	args := &producer.ProduceArgs{
		Src:            netip.MustParseAddrPort("192.0.2.1:4739"),
		SamplerAddress: netip.MustParseAddr("192.0.2.1"),
		TimeReceived:   time.Unix(1_700_000_001, 0).UTC(),
		FlowContext:    &ctx,
	}

	benchmarks := []struct {
		name   string
		format string
	}{
		{name: "protobuf_payload_for_kafka", format: "bin"},
		{name: "json", format: "json"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			formatter, err := format.FindFormat(bm.format)
			if err != nil {
				b.Fatalf("FindFormat returned error: %v", err)
			}
			var totalBytes int
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				packet := &netflow.IPFIXPacket{}
				if err := netflow.DecodeMessageVersion(bytes.NewBuffer(dataPayload), store, ctx, nil, packet); err != nil {
					b.Fatalf("DecodeMessageVersion returned error: %v", err)
				}
				messages, err := prod.Produce(packet, args)
				if err != nil {
					b.Fatalf("Produce returned error: %v", err)
				}
				if len(messages) != 1 {
					b.Fatalf("expected 1 message, got %d", len(messages))
				}
				_, payload, err := formatter.Format(messages[0])
				if err != nil {
					b.Fatalf("Format returned error: %v", err)
				}
				totalBytes += len(payload)
				prod.Commit(messages)
			}
			if totalBytes == 0 {
				b.Fatalf("expected formatted payload bytes")
			}
		})
	}
}

func benchOriginalIPFIXPacket() *netflow.IPFIXPacket {
	return &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          uint32(time.Unix(1_700_000_001, 0).Unix()),
		SequenceNumber:      42,
		ObservationDomainId: 7,
		FlowSets: []interface{}{
			netflow.DataFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 256},
				Records: []netflow.DataRecord{
					{
						Values: []netflow.DataField{
							{Type: netflow.IPFIX_FIELD_sourceIPv4Address, Value: []byte{192, 0, 2, 10}},
							{Type: netflow.IPFIX_FIELD_destinationIPv4Address, Value: []byte{198, 51, 100, 20}},
							{Type: netflow.IPFIX_FIELD_sourceTransportPort, Value: []byte{0x30, 0x39}},
							{Type: netflow.IPFIX_FIELD_destinationTransportPort, Value: []byte{0x01, 0xbb}},
							{Type: netflow.IPFIX_FIELD_protocolIdentifier, Value: []byte{6}},
							{Type: netflow.IPFIX_FIELD_octetDeltaCount, Value: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x41}},
							{Type: netflow.IPFIX_FIELD_packetDeltaCount, Value: []byte{0x00, 0x00, 0x00, 0x07}},
							{Type: netflow.IPFIX_FIELD_ingressInterface, Value: []byte{0x00, 0x00, 0x00, 0x09}},
							{Type: netflow.IPFIX_FIELD_egressInterface, Value: []byte{0x00, 0x00, 0x00, 0x0a}},
							{Type: netflow.IPFIX_FIELD_flowStartMilliseconds, Value: []byte{0x00, 0x00, 0x01, 0x8b, 0xcf, 0xe5, 0x6b, 0x64}},
							{Type: netflow.IPFIX_FIELD_flowEndMilliseconds, Value: []byte{0x00, 0x00, 0x01, 0x8b, 0xcf, 0xe5, 0x6e, 0x84}},
						},
					},
				},
			},
		},
	}
}

func benchOriginalIPFIXPayloads(b *testing.B) ([]byte, []byte) {
	b.Helper()
	templatePacket := &netflow.IPFIXPacket{
		Version:             10,
		ExportTime:          uint32(time.Unix(1_700_000_001, 0).Unix()),
		SequenceNumber:      41,
		ObservationDomainId: 7,
		FlowSets: []interface{}{
			netflow.TemplateFlowSet{
				FlowSetHeader: netflow.FlowSetHeader{Id: 2},
				Records: []netflow.TemplateRecord{
					{
						TemplateId: 256,
						FieldCount: 11,
						Fields: []netflow.Field{
							{Type: netflow.IPFIX_FIELD_sourceIPv4Address, Length: 4},
							{Type: netflow.IPFIX_FIELD_destinationIPv4Address, Length: 4},
							{Type: netflow.IPFIX_FIELD_sourceTransportPort, Length: 2},
							{Type: netflow.IPFIX_FIELD_destinationTransportPort, Length: 2},
							{Type: netflow.IPFIX_FIELD_protocolIdentifier, Length: 1},
							{Type: netflow.IPFIX_FIELD_octetDeltaCount, Length: 8},
							{Type: netflow.IPFIX_FIELD_packetDeltaCount, Length: 4},
							{Type: netflow.IPFIX_FIELD_ingressInterface, Length: 4},
							{Type: netflow.IPFIX_FIELD_egressInterface, Length: 4},
							{Type: netflow.IPFIX_FIELD_flowStartMilliseconds, Length: 8},
							{Type: netflow.IPFIX_FIELD_flowEndMilliseconds, Length: 8},
						},
					},
				},
			},
		},
	}
	dataPacket := benchOriginalIPFIXPacket()

	templatePayload, err := netflow.EncodeMessage(templatePacket)
	if err != nil {
		b.Fatalf("encode template packet: %v", err)
	}
	dataPayload, err := netflow.EncodeMessage(dataPacket)
	if err != nil {
		b.Fatalf("encode data packet: %v", err)
	}
	return templatePayload, dataPayload
}
