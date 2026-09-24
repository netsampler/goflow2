package protoproducer

import (
	"encoding/hex"
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

// benchPacketIPv4UDP is Ethernet + IPv4 + UDP/53 + a short DNS-like payload.
func benchPacketIPv4UDP() []byte {
	data, _ := hex.DecodeString("005300000001" + "005300000002" + "0800" +
		"45000064" + "abab" + "0000ff11" + "aaaa" + "0a000001" + "0a000002" +
		"ff00" + "0035" + "0015" + "ffff" +
		"02a901000001000000000000146578616d706c6503636f6d0000010001")
	return data
}

func BenchmarkParsePacketNoMapper(b *testing.B) {
	// Path taken by collectors that call ParseSampledHeader without a mapper:
	// ParsePacket with a nil config on the default environment.
	packets := map[string][]byte{
		"ipv6-tcp": benchSampledHeaderIPv6().HeaderData,
		"ipv4-udp": benchPacketIPv4UDP(),
	}
	for name, data := range packets {
		b.Run(name, func(b *testing.B) {
			msg := &ProtoProducerMessage{}
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				msg.Reset()
				if err := ParsePacket(msg, data, nil, DefaultEnvironment); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParsePacketEmptyMapper(b *testing.B) {
	// Path taken by the goflow2 binary with a default producer config: the
	// SFlowMapper exists but has no field mappings.
	mapper := mapFieldsSFlow(nil)
	mapper.parserEnvironment = NewBaseParserEnvironment()
	data := benchSampledHeaderIPv6().HeaderData
	msg := &ProtoProducerMessage{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		msg.Reset()
		if err := mapper.ParsePacket(msg, data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParsePacketCustomPort(b *testing.B) {
	// A parser registered on udp/53 so the port lookup and the port key are exercised.
	pe := NewBaseParserEnvironment()
	if err := pe.RegisterPort("udp", PortDirDst, 53, ParserInfo{
		Parser: func(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
			flowMessage.AddLayer("Custom")
			res.Size = len(data)
			return res, nil
		},
	}); err != nil {
		b.Fatal(err)
	}
	data := benchPacketIPv4UDP()
	msg := &ProtoProducerMessage{}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		msg.Reset()
		if err := ParsePacket(msg, data, nil, pe); err != nil {
			b.Fatal(err)
		}
	}
}
