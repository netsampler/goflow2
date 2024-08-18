package protoproducer

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/stretchr/testify/assert"
)

func TestMarshalJSON(t *testing.T) {
	var m ProtoProducerMessage

	m.formatter = &FormatterConfigMapper{
		fields: []string{"Etype", "test1", "test2", "test3"},
		rename: map[string]string{
			"Etype": "etype",
		},
		numToPb: map[int32]ProtobufFormatterConfig{
			100: ProtobufFormatterConfig{
				Name:  "test1",
				Index: 100,
				Type:  "varint",
				Array: false,
			},
			101: ProtobufFormatterConfig{
				Name:  "test2",
				Index: 101,
				Type:  "string",
				Array: false,
			},
			102: ProtobufFormatterConfig{
				Name:  "test3",
				Index: 102,
				Type:  "bytes",
				Array: false,
			},
		},
		render: map[string]RenderFunc{
			"Etype": EtypeRenderer,
			"test1": EtypeRenderer,
			"test2": NilRenderer,
			"test3": StringRenderer,
		},
	}

	m.FlowMessage.Etype = 0x86dd

	fmr := m.FlowMessage.ProtoReflect()
	unk := fmr.GetUnknown()

	unk = protowire.AppendTag(unk, protowire.Number(100), protowire.VarintType)
	unk = protowire.AppendVarint(unk, 0x86dd)

	unk = protowire.AppendTag(unk, protowire.Number(101), protowire.BytesType)
	unk = protowire.AppendString(unk, string("testing"))

	unk = protowire.AppendTag(unk, protowire.Number(102), protowire.BytesType)
	unk = protowire.AppendString(unk, string([]byte{0x74, 0x65, 0x73, 0x74, 0x69, 0x6e, 0x67}))

	fmr.SetUnknown(unk)

	out, err := m.MarshalJSON()
	assert.Nil(t, err)
	t.Log(string(out))
	assert.Equal(t, "{\"etype\":\"IPv6\",\"test1\":\"IPv6\",\"test2\":\"74657374696e67\",\"test3\":\"testing\"}", string(out))
}
