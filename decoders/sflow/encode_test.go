package sflow

import (
	"bytes"
	"testing"

	"github.com/netsampler/goflow2/v2/decoders/utils"
	"github.com/stretchr/testify/assert"
)

func TestEncodeDecodeSFlow(t *testing.T) {
	packet := Packet{
		Version:   5,
		IPVersion: 1,
		AgentIP:   utils.IPAddress{192, 0, 2, 1},
		SubAgentId:     1,
		SequenceNumber: 2,
		Uptime:         3,
		Samples: []interface{}{
			FlowSample{
				Header: SampleHeader{
					Format:              SAMPLE_FORMAT_FLOW,
					SampleSequenceNumber: 42,
					SourceIdType:         0,
					SourceIdValue:        7,
				},
				SamplingRate:     10,
				SamplePool:       20,
				Drops:            0,
				Input:            1,
				Output:           2,
				FlowRecordsCount: 1,
				Records: []FlowRecord{
					{
						Header: RecordHeader{
							DataFormat: FLOW_TYPE_RAW,
						},
						Data: SampledHeader{
							Protocol:       1,
							FrameLength:    64,
							Stripped:       0,
							OriginalLength: 64,
							HeaderData:     []byte{0xde, 0xad, 0xbe, 0xef},
						},
					},
				},
			},
		},
	}

	encoded, err := EncodeMessage(&packet)
	assert.NoError(t, err)

	var decoded Packet
	assert.NoError(t, DecodeMessageVersion(bytes.NewBuffer(encoded), &decoded))
	assert.Equal(t, uint32(5), decoded.Version)
	assert.Equal(t, uint32(1), decoded.IPVersion)
	assert.Equal(t, utils.IPAddress{192, 0, 2, 1}, decoded.AgentIP)
	assert.Len(t, decoded.Samples, 1)

	sample, ok := decoded.Samples[0].(FlowSample)
	assert.True(t, ok)
	assert.Equal(t, uint32(42), sample.Header.SampleSequenceNumber)
	assert.Equal(t, uint32(7), sample.Header.SourceIdValue)
	assert.Len(t, sample.Records, 1)

	record := sample.Records[0]
	assert.Equal(t, uint32(FLOW_TYPE_RAW), record.Header.DataFormat)
	header, ok := record.Data.(SampledHeader)
	assert.True(t, ok)
	assert.Equal(t, []byte{0xde, 0xad, 0xbe, 0xef}, header.HeaderData)
}
