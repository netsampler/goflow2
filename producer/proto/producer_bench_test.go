package protoproducer

import (
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/decoders/sflow"
)

type benchTemplateMapper struct {
	field MappableField
}

func (m benchTemplateMapper) Map(_ netflow.DataField) (MappableField, bool) {
	return m.field, true
}

func BenchmarkCustomMappingIPFIX(b *testing.B) {
	const benchFieldType uint16 = 9999
	df := netflow.DataField{
		Type:  benchFieldType,
		Value: []byte{0x00, 0x00, 0x00, 0x01},
	}

	b.Run("struct-field", func(b *testing.B) {
		mapper := mapFieldsNetFlow([]NetFlowMapField{
			{
				Type:        benchFieldType,
				Destination: "SrcAs",
				Endian:      BigEndian,
			},
		})
		msg := &ProtoProducerMessage{}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			msg.SrcAs = 0
			if err := MapCustomNetFlow(msg, df, mapper); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("unknown-proto", func(b *testing.B) {
		mapper := benchTemplateMapper{
			field: &MapConfigBase{
				ProtoIndex: 1000,
				ProtoType:  ProtoVarint,
				Endianness: BigEndian,
			},
		}
		msg := &ProtoProducerMessage{}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			*msg = ProtoProducerMessage{}
			if err := MapCustomNetFlow(msg, df, mapper); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkCustomMappingSFlow(b *testing.B) {
	sampledHeader := benchSampledHeaderIPv6()
	mapper := mapFieldsSFlow([]SFlowMapField{
		{
			Layer:       "etype0x86dd",
			Offset:      64,
			Length:      128,
			Destination: "SrcAddr",
			Endian:      BigEndian,
		},
	})
	msg := &ProtoProducerMessage{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg.SrcAddr = nil
		if err := ParseSampledHeaderConfig(msg, sampledHeader, mapper); err != nil {
			b.Fatal(err)
		}
	}
}

func benchSampledHeaderIPv6() *sflow.SampledHeader {
	return &sflow.SampledHeader{
		FrameLength: 10,
		Protocol:    1,
		HeaderData: []byte{
			0xff, 0xab, 0xcd, 0xef, 0xab, 0xcd, 0xff, 0xab, 0xcd, 0xef, 0xab, 0xbc, 0x86, 0xdd, 0x60, 0x2e,
			0xc4, 0xec, 0x01, 0xcc, 0x06, 0x40, 0xfd, 0x01, 0x00, 0x00, 0xff, 0x01, 0x82, 0x10, 0xcd, 0xff,
			0xff, 0x1c, 0x00, 0x00, 0x01, 0x50, 0xfd, 0x01, 0x00, 0x00, 0xff, 0x01, 0x00, 0x01, 0x02, 0xff,
			0xff, 0x93, 0x00, 0x00, 0x02, 0x46, 0xcf, 0xca, 0x00, 0x50, 0x05, 0x15, 0x21, 0x6f, 0xa4, 0x9c,
			0xf4, 0x59, 0x80, 0x18, 0x08, 0x09, 0x8c, 0x86, 0x00, 0x00, 0x01, 0x01, 0x08, 0x0a, 0x2a, 0x85,
			0xee, 0x9e, 0x64, 0x5c, 0x27, 0x28,
		},
	}
}
