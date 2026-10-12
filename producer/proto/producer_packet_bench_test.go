package protoproducer

import (
	"encoding/hex"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
)

// BenchmarkParseSampledHeader parses sampled headers into a reset message, as
// the sFlow producer does for every flow sample.
func BenchmarkParseSampledHeader(b *testing.B) {
	headers := []struct {
		name string
		hex  string
	}{
		{"eth-dot1q-ipv4-tcp", "005300000001" + "005300000002" + "8100" + "00640800" +
			"45000072abab40004006aaaa" + "0a000001" + "0a000002" +
			"c35001bb" + "00000001" + "00000002" + "8018ffff00000000" + "0101080a0000000100000002"},
		{"eth-ipv6-tcp", hex.EncodeToString(benchSampledHeaderIPv6().HeaderData)},
		{"eth-mpls-ipv4-udp", "005300000001" + "005300000002" + "8847" +
			"000640ff" + "000651ff" + // two MPLS labels, bottom of stack on the second
			"45000064abab0000ff11aaaa" + "0a000001" + "0a000002" +
			"ff0000350015ffff"},
	}
	for _, h := range headers {
		data, err := hex.DecodeString(h.hex)
		if err != nil {
			b.Fatal(err)
		}
		sampledHeader := &sflow.SampledHeader{Protocol: 1, HeaderData: data}
		b.Run(h.name, func(b *testing.B) {
			var msg ProtoProducerMessage
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				msg.Reset()
				if err := ParseSampledHeader(&msg, sampledHeader); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
