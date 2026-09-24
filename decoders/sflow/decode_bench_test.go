package sflow

import (
	"bytes"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/utils"
)

// benchHeaderData is Ethernet + IPv4 + TCP with a payload, 128 bytes like a
// typical sampled header.
func benchHeaderData() []byte {
	hdr := []byte{
		0x00, 0x53, 0x00, 0x00, 0x00, 0x01, 0x00, 0x53, 0x00, 0x00, 0x00, 0x02, 0x08, 0x00, // ethernet
		0x45, 0x00, 0x00, 0x72, 0xab, 0xab, 0x40, 0x00, 0x40, 0x06, 0xaa, 0xaa, // ipv4
		0x0a, 0x00, 0x00, 0x01, 0x0a, 0x00, 0x00, 0x02,
		0xc3, 0x50, 0x01, 0xbb, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02, // tcp
		0x80, 0x18, 0xff, 0xff, 0x00, 0x00, 0x00, 0x00,
		0x01, 0x01, 0x08, 0x0a, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x02,
	}
	for len(hdr) < 128 {
		hdr = append(hdr, byte(len(hdr)))
	}
	return hdr
}

// benchFlowDatagram builds a datagram with samples flow samples, each with the
// record mix seen on production routers: raw header, extended switch, extended
// router and extended gateway.
func benchFlowDatagram(b *testing.B, samples int) []byte {
	b.Helper()
	packet := Packet{
		Version:        5,
		IPVersion:      1,
		AgentIP:        utils.IPAddress{198, 51, 100, 1},
		SubAgentId:     1,
		SequenceNumber: 2,
		Uptime:         3,
		SamplesCount:   uint32(samples),
	}
	for i := 0; i < samples; i++ {
		packet.Samples = append(packet.Samples, FlowSample{
			Header: SampleHeader{
				Format:               SAMPLE_FORMAT_FLOW,
				SampleSequenceNumber: uint32(100 + i),
				SourceIdType:         0,
				SourceIdValue:        uint32(1 + i),
			},
			SamplingRate:     2048,
			SamplePool:       uint32(2048 * (100 + i)),
			Input:            10,
			Output:           20,
			FlowRecordsCount: 4,
			Records: []FlowRecord{
				{Data: SampledHeader{Protocol: 1, FrameLength: 1518, Stripped: 4, HeaderData: benchHeaderData()}},
				{Data: ExtendedSwitch{SrcVlan: 100, DstVlan: 200}},
				{Data: ExtendedRouter{NextHop: utils.IPAddress{203, 0, 113, 1}, SrcMaskLen: 24, DstMaskLen: 22}},
				{Data: ExtendedGateway{
					NextHop:     utils.IPAddress{203, 0, 113, 1},
					AS:          64512,
					SrcAS:       64513,
					SrcPeerAS:   64514,
					DstASPath:   []ASPathSegment{{Type: 2, Path: []uint32{64515, 64516, 64517}}},
					Communities: []uint32{100, 200, 300},
					LocalPref:   100,
				}},
			},
		})
	}
	encoded, err := EncodeMessage(&packet)
	if err != nil {
		b.Fatal(err)
	}
	return encoded
}

func benchCounterDatagram(b *testing.B) []byte {
	b.Helper()
	packet := Packet{
		Version:      5,
		IPVersion:    1,
		AgentIP:      utils.IPAddress{198, 51, 100, 1},
		SamplesCount: 1,
		Samples: []interface{}{
			CounterSample{
				Header:              SampleHeader{Format: SAMPLE_FORMAT_COUNTER, SampleSequenceNumber: 1, SourceIdValue: 1},
				CounterRecordsCount: 2,
				Records: []CounterRecord{
					{Data: IfCounters{IfIndex: 1, IfType: 6, IfSpeed: 10_000_000_000, IfInOctets: 1 << 40, IfOutOctets: 1 << 41}},
					{Data: EthernetCounters{Dot3StatsFCSErrors: 3}},
				},
			},
		},
	}
	encoded, err := EncodeMessage(&packet)
	if err != nil {
		b.Fatal(err)
	}
	return encoded
}

func benchmarkDecode(b *testing.B, data []byte, wantSamples int) {
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		var packet Packet
		if err := DecodeMessageVersion(bytes.NewBuffer(data), &packet); err != nil {
			b.Fatal(err)
		}
		if len(packet.Samples) != wantSamples {
			b.Fatalf("got %d samples, want %d", len(packet.Samples), wantSamples)
		}
	}
}

func BenchmarkDecodeMessageFlowSamples1(b *testing.B) {
	benchmarkDecode(b, benchFlowDatagram(b, 1), 1)
}

func BenchmarkDecodeMessageFlowSamples4(b *testing.B) {
	benchmarkDecode(b, benchFlowDatagram(b, 4), 4)
}

func BenchmarkDecodeMessageCounterSample(b *testing.B) {
	benchmarkDecode(b, benchCounterDatagram(b), 1)
}
