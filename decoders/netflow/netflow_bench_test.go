package netflow

import (
	"bytes"
	"testing"
)

// benchIPFIXDataPacket builds a template packet and a data packet with three
// records using the encoder, so the benchmark does not depend on a capture.
func benchIPFIXDataPacket(b *testing.B) (template, data []byte) {
	b.Helper()

	fields := []Field{
		{Type: IPFIX_FIELD_sourceIPv4Address, Length: 4},
		{Type: IPFIX_FIELD_destinationIPv4Address, Length: 4},
		{Type: IPFIX_FIELD_sourceTransportPort, Length: 2},
		{Type: IPFIX_FIELD_destinationTransportPort, Length: 2},
		{Type: IPFIX_FIELD_protocolIdentifier, Length: 1},
		{Type: IPFIX_FIELD_octetDeltaCount, Length: 8},
		{Type: IPFIX_FIELD_packetDeltaCount, Length: 8},
		{Type: IPFIX_FIELD_ingressInterface, Length: 4},
	}
	record := DataRecord{Values: []DataField{
		{Type: IPFIX_FIELD_sourceIPv4Address, Value: []byte{10, 0, 0, 1}},
		{Type: IPFIX_FIELD_destinationIPv4Address, Value: []byte{10, 0, 0, 2}},
		{Type: IPFIX_FIELD_sourceTransportPort, Value: []byte{0xc3, 0x50}},
		{Type: IPFIX_FIELD_destinationTransportPort, Value: []byte{0x01, 0xbb}},
		{Type: IPFIX_FIELD_protocolIdentifier, Value: []byte{6}},
		{Type: IPFIX_FIELD_octetDeltaCount, Value: []byte{0, 0, 0, 0, 0, 0, 0x10, 0}},
		{Type: IPFIX_FIELD_packetDeltaCount, Value: []byte{0, 0, 0, 0, 0, 0, 0, 8}},
		{Type: IPFIX_FIELD_ingressInterface, Value: []byte{0, 0, 0, 1}},
	}}

	templatePacket := IPFIXPacket{
		Version:             10,
		ObservationDomainId: 1,
		FlowSets: []interface{}{
			TemplateFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 2},
				Records:       []TemplateRecord{{TemplateId: 256, FieldCount: uint16(len(fields)), Fields: fields}},
			},
		},
	}
	dataPacket := IPFIXPacket{
		Version:             10,
		ObservationDomainId: 1,
		FlowSets: []interface{}{
			// Two data sets so the per-flowset header cost shows up too.
			DataFlowSet{FlowSetHeader: FlowSetHeader{Id: 256}, Records: []DataRecord{record, record}},
			DataFlowSet{FlowSetHeader: FlowSetHeader{Id: 256}, Records: []DataRecord{record}},
		},
	}

	var err error
	if template, err = EncodeMessage(&templatePacket); err != nil {
		b.Fatalf("encode template: %v", err)
	}
	if data, err = EncodeMessage(&dataPacket); err != nil {
		b.Fatalf("encode data: %v", err)
	}
	return template, data
}

func BenchmarkDecodeMessageIPFIX(b *testing.B) {
	template, data := benchIPFIXDataPacket(b)
	store := newTestTemplateStore()
	ctx := FlowContext{RouterKey: "router1"}

	var packet IPFIXPacket
	if err := DecodeMessageVersion(bytes.NewBuffer(template), store, ctx, nil, &packet); err != nil {
		b.Fatalf("decode template: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var packet IPFIXPacket
		if err := DecodeMessageVersion(bytes.NewBuffer(data), store, ctx, nil, &packet); err != nil {
			b.Fatalf("decode data: %v", err)
		}
	}
}
