package netflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeDataSetSharesOneArena(t *testing.T) {
	fields := []Field{
		{Type: IPFIX_FIELD_sourceTransportPort, Length: 2},
		{Type: IPFIX_FIELD_protocolIdentifier, Length: 1},
	}
	payload := []byte{
		0x00, 0x50, 6, // record 0
		0x01, 0xbb, 17, // record 1
		0x00, 0x35, 6, // record 2
	}

	records, err := DecodeDataSet(10, bytes.NewBuffer(payload), fields)
	require.NoError(t, err)
	require.Len(t, records, 3)

	assert.Equal(t, []byte{0x00, 0x50}, records[0].Values[0].Value)
	assert.Equal(t, []byte{6}, records[0].Values[1].Value)
	assert.Equal(t, []byte{0x01, 0xbb}, records[1].Values[0].Value)
	assert.Equal(t, []byte{17}, records[1].Values[1].Value)
	assert.Equal(t, []byte{0x00, 0x35}, records[2].Values[0].Value)
	assert.Equal(t, uint16(IPFIX_FIELD_protocolIdentifier), records[2].Values[1].Type)

	// Records share a backing array but each has clipped capacity, so appending
	// to one record must not overwrite the next one.
	assert.Equal(t, len(records[0].Values), cap(records[0].Values))
	records[0].Values = append(records[0].Values, DataField{Type: 9999})
	assert.Equal(t, uint16(IPFIX_FIELD_sourceTransportPort), records[1].Values[0].Type)
	assert.Equal(t, []byte{0x01, 0xbb}, records[1].Values[0].Value)

	// Values are sub-slices of the payload, not copies.
	payload[0], payload[1] = 0xaa, 0xbb
	assert.Equal(t, []byte{0xaa, 0xbb}, records[0].Values[0].Value)
}

func TestDecodeDataSetVariableLength(t *testing.T) {
	fields := []Field{
		{Type: IPFIX_FIELD_protocolIdentifier, Length: 1},
		{Type: IPFIX_FIELD_interfaceName, Length: 0xffff},
	}
	long := bytes.Repeat([]byte{'x'}, 300)

	var payload []byte
	payload = append(payload, 6, 3, 'e', 't', 'h')  // short form: 1-byte length
	payload = append(payload, 17, 0xff, 0x01, 0x2c) // extended form: 0xff + uint16(300)
	payload = append(payload, long...)              //
	payload = append(payload, 1, 0)                 // empty variable-length value

	records, err := DecodeDataSet(10, bytes.NewBuffer(payload), fields)
	require.NoError(t, err)
	require.Len(t, records, 3)

	assert.Equal(t, []byte{6}, records[0].Values[0].Value)
	assert.Equal(t, []byte("eth"), records[0].Values[1].Value)
	assert.Equal(t, []byte{17}, records[1].Values[0].Value)
	assert.Equal(t, long, records[1].Values[1].Value)
	assert.Equal(t, []byte{1}, records[2].Values[0].Value)
	assert.Empty(t, records[2].Values[1].Value)
}

func TestDecodeDataSetVariableLengthTruncated(t *testing.T) {
	fields := []Field{{Type: IPFIX_FIELD_interfaceName, Length: 0xffff}}

	// Extended length marker without the two length bytes.
	_, err := DecodeDataSet(10, bytes.NewBuffer([]byte{0xff}), fields)
	assert.Error(t, err)
}

func TestDecodeDataSetEmptyTemplate(t *testing.T) {
	// A template without fields would never consume payload; it must fail
	// instead of looping forever.
	_, err := DecodeDataSet(10, bytes.NewBuffer([]byte{1, 2, 3, 4}), nil)
	assert.Error(t, err)

	_, err = DecodeOptionsDataSet(10, bytes.NewBuffer([]byte{1, 2, 3, 4}), nil, nil)
	assert.Error(t, err)

	// Without payload there is nothing to decode and no error.
	records, err := DecodeDataSet(10, bytes.NewBuffer(nil), nil)
	assert.NoError(t, err)
	assert.Empty(t, records)
}

func TestDecodeDataSetUsingFieldsShortPayload(t *testing.T) {
	fields := []Field{
		{Type: IPFIX_FIELD_sourceTransportPort, Length: 2},
		{Type: IPFIX_FIELD_protocolIdentifier, Length: 1},
	}

	// Legacy behaviour: a payload shorter than the template yields zero-value fields.
	values, err := DecodeDataSetUsingFields(10, bytes.NewBuffer([]byte{0x00}), fields)
	require.NoError(t, err)
	assert.Equal(t, []DataField{{}, {}}, values)

	values, err = DecodeDataSetUsingFields(10, bytes.NewBuffer([]byte{0x00, 0x50, 6}), fields)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x00, 0x50}, values[0].Value)
	assert.Equal(t, []byte{6}, values[1].Value)
}

func TestDecodeOptionsDataSetSharesOneArena(t *testing.T) {
	scopes := []Field{{Type: IPFIX_FIELD_observationDomainId, Length: 4}}
	options := []Field{{Type: IPFIX_FIELD_samplingInterval, Length: 2}}
	payload := []byte{
		0, 0, 0, 1, 0x03, 0xe8, // record 0: domain 1, interval 1000
		0, 0, 0, 2, 0x00, 0x64, // record 1: domain 2, interval 100
	}

	records, err := DecodeOptionsDataSet(10, bytes.NewBuffer(payload), scopes, options)
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.Equal(t, []byte{0, 0, 0, 1}, records[0].ScopesValues[0].Value)
	assert.Equal(t, []byte{0x03, 0xe8}, records[0].OptionsValues[0].Value)
	assert.Equal(t, []byte{0, 0, 0, 2}, records[1].ScopesValues[0].Value)
	assert.Equal(t, []byte{0x00, 0x64}, records[1].OptionsValues[0].Value)

	records[0].ScopesValues = append(records[0].ScopesValues, DataField{Type: 9999})
	assert.Equal(t, uint16(IPFIX_FIELD_samplingInterval), records[0].OptionsValues[0].Type)
	assert.Equal(t, uint16(IPFIX_FIELD_observationDomainId), records[1].ScopesValues[0].Type)
}

func TestEstimateRecords(t *testing.T) {
	assert.Equal(t, 0, estimateRecords(100, 0))
	assert.Equal(t, 10, estimateRecords(100, 10))
	assert.Equal(t, 3, estimateRecords(100, 30))
	assert.Equal(t, maxPreallocatedRecords, estimateRecords(1<<20, 1))
}
