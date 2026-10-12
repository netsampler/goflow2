package netflow

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncodeDecodeNetFlowV9(t *testing.T) {
	packet := NFv9Packet{
		Version:        9,
		Count:          4,
		SystemUptime:   100,
		UnixSeconds:    200,
		SequenceNumber: 300,
		SourceId:       400,
		FlowSets: []interface{}{
			TemplateFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 0},
				Records: []TemplateRecord{
					{
						TemplateId: 256,
						FieldCount: 2,
						Fields: []Field{
							{Type: NFV9_FIELD_IN_BYTES, Length: 4},
							{Type: NFV9_FIELD_IPV4_SRC_ADDR, Length: 4},
						},
					},
				},
			},
			NFv9OptionsTemplateFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 1},
				Records: []NFv9OptionsTemplateRecord{
					{
						TemplateId:   257,
						ScopeLength:  4,
						OptionLength: 4,
						Scopes: []Field{
							{Type: 1, Length: 4},
						},
						Options: []Field{
							{Type: NFV9_FIELD_SAMPLING_INTERVAL, Length: 4},
						},
					},
				},
			},
			DataFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 256},
				Records: []DataRecord{
					{
						Values: []DataField{
							{Type: NFV9_FIELD_IN_BYTES, Value: []byte{0, 0, 0, 10}},
							{Type: NFV9_FIELD_IPV4_SRC_ADDR, Value: []byte{192, 0, 2, 1}},
						},
					},
				},
			},
			OptionsDataFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 257},
				Records: []OptionsDataRecord{
					{
						ScopesValues: []DataField{
							{Type: 1, Value: []byte{0, 0, 0, 1}},
						},
						OptionsValues: []DataField{
							{Type: NFV9_FIELD_SAMPLING_INTERVAL, Value: []byte{0, 0, 3, 232}},
						},
					},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)

	store := newTestTemplateStore()
	ctx := FlowContext{RouterKey: "test-router"}
	var decoded NFv9Packet
	assert.NoError(t, DecodeMessageVersion(bytes.NewBuffer(encoded), store, ctx, &decoded, nil))

	assert.Equal(t, packet.Version, decoded.Version)
	assert.Equal(t, packet.Count, decoded.Count)
	assert.Equal(t, packet.SystemUptime, decoded.SystemUptime)
	assert.Equal(t, packet.UnixSeconds, decoded.UnixSeconds)
	assert.Equal(t, packet.SequenceNumber, decoded.SequenceNumber)
	assert.Equal(t, packet.SourceId, decoded.SourceId)
	assert.Len(t, decoded.FlowSets, 4)

	templateSet, ok := decoded.FlowSets[0].(TemplateFlowSet)
	assert.True(t, ok)
	assert.Equal(t, uint16(0), templateSet.Id)
	assert.Equal(t, []Field{
		{Type: NFV9_FIELD_IN_BYTES, Length: 4},
		{Type: NFV9_FIELD_IPV4_SRC_ADDR, Length: 4},
	}, templateSet.Records[0].Fields)

	optionsTemplateSet, ok := decoded.FlowSets[1].(NFv9OptionsTemplateFlowSet)
	assert.True(t, ok)
	assert.Equal(t, uint16(1), optionsTemplateSet.Id)
	assert.Equal(t, []Field{{Type: 1, Length: 4}}, optionsTemplateSet.Records[0].Scopes)
	assert.Equal(t, []Field{{Type: NFV9_FIELD_SAMPLING_INTERVAL, Length: 4}}, optionsTemplateSet.Records[0].Options)

	dataSet, ok := decoded.FlowSets[2].(DataFlowSet)
	assert.True(t, ok)
	assert.Equal(t, uint16(256), dataSet.Id)
	assert.Equal(t, []byte{0, 0, 0, 10}, dataSet.Records[0].Values[0].Value)
	assert.Equal(t, []byte{192, 0, 2, 1}, dataSet.Records[0].Values[1].Value)

	optionsDataSet, ok := decoded.FlowSets[3].(OptionsDataFlowSet)
	assert.True(t, ok)
	assert.Equal(t, uint16(257), optionsDataSet.Id)
	assert.Equal(t, []byte{0, 0, 0, 1}, optionsDataSet.Records[0].ScopesValues[0].Value)
	assert.Equal(t, []byte{0, 0, 3, 232}, optionsDataSet.Records[0].OptionsValues[0].Value)
}

func TestEncodeDecodeIPFIX(t *testing.T) {
	packet := IPFIXPacket{
		Version:             10,
		ExportTime:          123,
		SequenceNumber:      456,
		ObservationDomainId: 789,
		FlowSets: []interface{}{
			TemplateFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 2},
				Records: []TemplateRecord{
					{
						TemplateId: 300,
						FieldCount: 2,
						Fields: []Field{
							{Type: IPFIX_FIELD_octetDeltaCount, Length: 8},
							{PenProvided: true, Type: 4000, Length: 2, Pen: 32473},
						},
					},
				},
			},
			IPFIXOptionsTemplateFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 3},
				Records: []IPFIXOptionsTemplateRecord{
					{
						TemplateId:      301,
						FieldCount:      2,
						ScopeFieldCount: 1,
						Scopes: []Field{
							{Type: IPFIX_FIELD_observationDomainId, Length: 4},
						},
						Options: []Field{
							{Type: IPFIX_FIELD_samplingInterval, Length: 0xffff},
						},
					},
				},
			},
			DataFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 300},
				Records: []DataRecord{
					{
						Values: []DataField{
							{Type: IPFIX_FIELD_octetDeltaCount, Value: []byte{0, 0, 0, 0, 0, 0, 0, 5}},
							{PenProvided: true, Type: 4000, Pen: 32473, Value: []byte{0x12, 0x34}},
						},
					},
				},
			},
			OptionsDataFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 301},
				Records: []OptionsDataRecord{
					{
						ScopesValues: []DataField{
							{Type: IPFIX_FIELD_observationDomainId, Value: []byte{0, 0, 3, 21}},
						},
						OptionsValues: []DataField{
							{Type: IPFIX_FIELD_samplingInterval, Value: []byte("abcde")},
						},
					},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)

	store := newTestTemplateStore()
	ctx := FlowContext{RouterKey: "test-router"}
	var decoded IPFIXPacket
	assert.NoError(t, DecodeMessageVersion(bytes.NewBuffer(encoded), store, ctx, nil, &decoded))

	assert.Equal(t, uint16(10), decoded.Version)
	assert.Equal(t, uint16(len(encoded)), decoded.Length)
	assert.Equal(t, packet.ExportTime, decoded.ExportTime)
	assert.Equal(t, packet.SequenceNumber, decoded.SequenceNumber)
	assert.Equal(t, packet.ObservationDomainId, decoded.ObservationDomainId)
	assert.Len(t, decoded.FlowSets, 4)

	templateSet, ok := decoded.FlowSets[0].(TemplateFlowSet)
	assert.True(t, ok)
	assert.Equal(t, uint16(2), templateSet.Id)
	assert.Equal(t, []Field{
		{Type: IPFIX_FIELD_octetDeltaCount, Length: 8},
		{PenProvided: true, Type: 4000, Length: 2, Pen: 32473},
	}, templateSet.Records[0].Fields)

	optionsTemplateSet, ok := decoded.FlowSets[1].(IPFIXOptionsTemplateFlowSet)
	assert.True(t, ok)
	assert.Equal(t, uint16(3), optionsTemplateSet.Id)
	assert.Equal(t, []Field{{Type: IPFIX_FIELD_observationDomainId, Length: 4}}, optionsTemplateSet.Records[0].Scopes)
	assert.Equal(t, []Field{{Type: IPFIX_FIELD_samplingInterval, Length: 0xffff}}, optionsTemplateSet.Records[0].Options)

	dataSet, ok := decoded.FlowSets[2].(DataFlowSet)
	assert.True(t, ok)
	assert.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 5}, dataSet.Records[0].Values[0].Value)
	assert.Equal(t, []byte{0x12, 0x34}, dataSet.Records[0].Values[1].Value)

	optionsDataSet, ok := decoded.FlowSets[3].(OptionsDataFlowSet)
	assert.True(t, ok)
	assert.Equal(t, []byte{0, 0, 3, 21}, optionsDataSet.Records[0].ScopesValues[0].Value)
	assert.Equal(t, []byte("abcde"), optionsDataSet.Records[0].OptionsValues[0].Value)
}

func TestEncodeIPFIXLengthsUseEmittedVariableLengthSection(t *testing.T) {
	section := bytes.Repeat([]byte{0xab}, 88)
	packet := IPFIXPacket{
		Version:             10,
		Length:              856,
		ExportTime:          123,
		SequenceNumber:      456,
		ObservationDomainId: 789,
		FlowSets: []interface{}{
			TemplateFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 2},
				Records: []TemplateRecord{
					{
						TemplateId: 256,
						Fields: []Field{
							{Type: IPFIX_FIELD_octetDeltaCount, Length: 8},
							{Type: IPFIX_FIELD_dataLinkFrameSize, Length: 2},
							{Type: IPFIX_FIELD_dataLinkFrameSection, Length: 0xffff},
						},
					},
				},
			},
			DataFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 256},
				Records: []DataRecord{
					{
						Values: []DataField{
							{Type: IPFIX_FIELD_octetDeltaCount, Value: []byte{0, 0, 0, 0, 0, 0, 0, 1}},
							{Type: IPFIX_FIELD_dataLinkFrameSize, Value: []byte{0, 0x60}},
							{Type: IPFIX_FIELD_dataLinkFrameSection, Value: section},
						},
					},
					{
						Values: []DataField{
							{Type: IPFIX_FIELD_octetDeltaCount, Value: []byte{0, 0, 0, 0, 0, 0, 0, 2}},
							{Type: IPFIX_FIELD_dataLinkFrameSize, Value: []byte{0, 0x60}},
							{Type: IPFIX_FIELD_dataLinkFrameSection, Value: section},
						},
					},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)
	assert.Equal(t, len(encoded), int(binary.BigEndian.Uint16(encoded[2:4])))

	offset := ipfixHeaderLen
	templateSetLength := int(binary.BigEndian.Uint16(encoded[offset+2 : offset+4]))
	assert.Equal(t, offset+templateSetLength, nextSetOffset(t, encoded, offset))
	offset += templateSetLength

	dataSetLength := int(binary.BigEndian.Uint16(encoded[offset+2 : offset+4]))
	recordLength := 8 + 2 + 1 + len(section)
	padding := (4 - ((flowSetHeaderLen + recordLength*2) % 4)) % 4
	assert.Equal(t, flowSetHeaderLen+recordLength*2+padding, dataSetLength)
	assert.Equal(t, len(encoded), offset+dataSetLength)

	recordOffset := offset + flowSetHeaderLen
	assert.Equal(t, byte(0x58), encoded[recordOffset+8+2])
	assert.Equal(t, section, encoded[recordOffset+8+2+1:recordOffset+recordLength])

	recordOffset += recordLength
	assert.Equal(t, byte(0x58), encoded[recordOffset+8+2])
	assert.Equal(t, section, encoded[recordOffset+8+2+1:recordOffset+recordLength])
}

func TestEncodeIPFIXPayloadAggregateFrameSectionBoundary(t *testing.T) {
	record := func(frameSize uint16, section []byte) DataRecord {
		fixed := make([]byte, 99)
		return DataRecord{
			Values: []DataField{
				{Type: IPFIX_FIELD_dataLinkFrameSize, Length: 2, Value: []byte{byte(frameSize >> 8), byte(frameSize)}},
				{Type: IPFIX_FIELD_octetTotalCount, Length: uint16(len(fixed)), Value: fixed},
				{Type: IPFIX_FIELD_dataLinkFrameSection, Length: 0xffff, Value: section},
			},
		}
	}
	packet := IPFIXPacket{
		Version:             10,
		Length:              0x0408,
		ExportTime:          123,
		SequenceNumber:      456,
		ObservationDomainId: 789,
		FlowSets: []interface{}{
			DataFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 256},
				Records: []DataRecord{
					record(74, bytes.Repeat([]byte{0xaa}, 74)),
					record(96, bytes.Repeat([]byte{0xbb}, 96)),
					record(96, bytes.Repeat([]byte{0xcc}, 88)),
				},
			},
			DataFlowSet{
				FlowSetHeader: FlowSetHeader{Id: 257},
				Records: []DataRecord{
					{
						Values: []DataField{
							{Type: IPFIX_FIELD_dataLinkFrameSection, Length: 436, Value: bytes.Repeat([]byte{0xdd}, 436)},
						},
					},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)
	assert.Equal(t, 0x0400, int(binary.BigEndian.Uint16(encoded[2:4])))
	assert.Equal(t, 1024, len(encoded))

	set256Offset := ipfixHeaderLen
	assert.Equal(t, 256, int(binary.BigEndian.Uint16(encoded[set256Offset:set256Offset+2])))
	assert.Equal(t, 0x0238, int(binary.BigEndian.Uint16(encoded[set256Offset+2:set256Offset+4])))
	record3Offset := set256Offset + flowSetHeaderLen + (101 + 1 + 74) + (101 + 1 + 96)
	assert.Equal(t, byte(0x58), encoded[record3Offset+101])

	set257Offset := set256Offset + int(binary.BigEndian.Uint16(encoded[set256Offset+2:set256Offset+4]))
	assert.Equal(t, 584, set257Offset)
	assert.Equal(t, 257, int(binary.BigEndian.Uint16(encoded[set257Offset:set257Offset+2])))
	assert.Equal(t, 0x01b8, int(binary.BigEndian.Uint16(encoded[set257Offset+2:set257Offset+4])))
	assert.Equal(t, len(encoded), set257Offset+int(binary.BigEndian.Uint16(encoded[set257Offset+2:set257Offset+4])))
}

func nextSetOffset(t *testing.T, encoded []byte, offset int) int {
	t.Helper()
	if offset+flowSetHeaderLen > len(encoded) {
		t.Fatalf("set header at offset %d exceeds message length %d", offset, len(encoded))
	}
	length := int(binary.BigEndian.Uint16(encoded[offset+2 : offset+4]))
	if length < flowSetHeaderLen {
		t.Fatalf("set at offset %d has invalid length %d", offset, length)
	}
	if offset+length > len(encoded) {
		t.Fatalf("set at offset %d length %d exceeds message length %d", offset, length, len(encoded))
	}
	return offset + length
}
