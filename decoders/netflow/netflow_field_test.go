package netflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecodeFieldEnterpriseBit(t *testing.T) {
	// Enterprise bit set, type 100, length 4, PEN 9.
	payload := bytes.NewBuffer([]byte{0x80, 0x64, 0x00, 0x04, 0x00, 0x00, 0x00, 0x09})
	var field Field
	assert.NoError(t, DecodeField(payload, &field, true))
	assert.Equal(t, Field{PenProvided: true, Type: 100, Length: 4, Pen: 9}, field)

	// Without enterprise numbers (NetFlow v9), the type is kept as is.
	payload = bytes.NewBuffer([]byte{0x80, 0x64, 0x00, 0x04})
	field = Field{}
	assert.NoError(t, DecodeField(payload, &field, false))
	assert.Equal(t, Field{Type: 0x8064, Length: 4}, field)
}

func TestDecodeIPFIXOptionsTemplateSetEnterpriseBit(t *testing.T) {
	payload := bytes.NewBuffer([]byte{
		0x01, 0x00, 0x00, 0x02, 0x00, 0x01, // template 256, 2 fields, 1 scope
		0x01, 0x2e, 0x00, 0x04, // selectorId, length 4
		0xaf, 0xc8, 0x00, 0x20, 0x00, 0x00, 0x00, 0x09, // 9:12232, length 32
	})
	records, err := DecodeIPFIXOptionsTemplateSet(payload)
	assert.NoError(t, err)
	assert.Len(t, records, 1)
	assert.Equal(t, []Field{{PenProvided: true, Type: 12232, Length: 32, Pen: 9}}, records[0].Options)
}
