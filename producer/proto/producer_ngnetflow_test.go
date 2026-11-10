package protoproducer

import (
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/stretchr/testify/assert"
)

func u32(v uint32) []byte { return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)} }

// FreeBSD ng_netflow (OPNsense, pfSense) puts OUT_PKTS/OUT_BYTES after IN_PKTS/IN_BYTES in its
// v9 template and always sends them as 0. The zero OUT counters must not erase the IN ones.
func TestNetFlowV9ZeroOutCountersKeepIn(t *testing.T) {
	cases := []struct {
		name                   string
		inB, inP, outB, outP   uint32
		wantBytes, wantPackets uint64
	}{
		{"ng_netflow: IN set, OUT zero", 4774, 7, 0, 0, 4774, 7},
		{"egress-only: IN zero, OUT set", 0, 0, 9000, 12, 9000, 12},
		{"both set: OUT still wins", 4774, 7, 9000, 12, 9000, 12},
		{"both zero", 0, 0, 0, 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var m ProtoProducerMessage
			err := ConvertNetFlowDataSet(&m, 9, 1704067200, 2000, []netflow.DataField{
				{Type: netflow.NFV9_FIELD_IN_PKTS, Value: u32(c.inP)},
				{Type: netflow.NFV9_FIELD_IN_BYTES, Value: u32(c.inB)},
				{Type: netflow.NFV9_FIELD_OUT_PKTS, Value: u32(c.outP)},
				{Type: netflow.NFV9_FIELD_OUT_BYTES, Value: u32(c.outB)},
			}, nil, nil)
			assert.Nil(t, err)
			assert.Equal(t, c.wantBytes, m.Bytes)
			assert.Equal(t, c.wantPackets, m.Packets)
		})
	}
}
