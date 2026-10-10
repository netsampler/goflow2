package netflow

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeFieldEnterpriseOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire []byte
		pen  bool
		want Field
	}{
		{"enterprise IPFIX", []byte{0x80, 0x64, 0, 4, 0, 0, 0, 9}, true, Field{PenProvided: true, Type: 100, Length: 4, Pen: 9}},
		{"standard IPFIX", []byte{0, 0x64, 0, 4}, true, Field{Type: 100, Length: 4}},
		{"NetFlow v9 high bit", []byte{0x80, 0x64, 0, 4}, false, Field{Type: 0x8064, Length: 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := bytes.NewBuffer(tc.wire)
			var field Field
			require.NoError(t, DecodeField(payload, &field, tc.pen))
			require.Equal(t, tc.want, field)
			require.Zero(t, payload.Len())
		})
	}
}

func TestDecodeIPFIXEnterpriseOptionsData(t *testing.T) {
	// Enterprise scope 9:100, enterprise option 9:12232, standard samplingInterval.
	template, err := DecodeIPFIXOptionsTemplateSet(bytes.NewBuffer([]byte{
		1, 0, 0, 3, 0, 1,
		0x80, 0x64, 0, 4, 0, 0, 0, 9,
		0xaf, 0xc8, 0, 4, 0, 0, 0, 9,
		0, 34, 0, 4,
	}))
	require.NoError(t, err)
	require.Len(t, template, 1)
	require.Equal(t, []Field{{PenProvided: true, Type: 100, Length: 4, Pen: 9}}, template[0].Scopes)
	require.Equal(t, []Field{{PenProvided: true, Type: 12232, Length: 4, Pen: 9}, {Type: 34, Length: 4}}, template[0].Options)
	records, err := DecodeOptionsDataSet(10, bytes.NewBuffer([]byte{0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 3, 232}), template[0].Scopes, template[0].Options)
	require.NoError(t, err)
	require.Equal(t, []OptionsDataRecord{{
		ScopesValues:  []DataField{{PenProvided: true, Type: 100, Pen: 9, Value: []byte{0, 0, 0, 1}}},
		OptionsValues: []DataField{{PenProvided: true, Type: 12232, Pen: 9, Value: []byte{0, 0, 0, 2}}, {Type: 34, Value: []byte{0, 0, 3, 232}}},
	}}, records)
}
