package protoproducer

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/proto"

	"github.com/netsampler/goflow2/v3/decoders/sflow"
	"github.com/netsampler/goflow2/v3/decoders/utils"
)

// marshalTestMessage returns the message produced for an sFlow flow sample
// with a sampled Ethernet/802.1Q/IPv4/TCP header and the extended switch,
// router and gateway records.
func marshalTestMessage(t testing.TB) *ProtoProducerMessage {
	t.Helper()
	header, err := hex.DecodeString("005300000001" + "005300000002" + "8100" + "00640800" +
		"45000072abab40004006aaaa" + "0a000001" + "0a000002" +
		"c35001bb" + "00000001" + "00000002" + "8018ffff00000000" +
		"0101080a0000000100000002")
	require.NoError(t, err)
	packet := sflow.Packet{
		AgentIP:        utils.IPAddress{198, 51, 100, 1},
		SequenceNumber: 2,
		Samples: []interface{}{
			sflow.FlowSample{
				SamplingRate: 2048,
				Input:        10,
				Output:       20,
				Records: []sflow.FlowRecord{
					{Data: sflow.SampledHeader{Protocol: 1, FrameLength: 1518, HeaderData: header}},
					{Data: sflow.ExtendedSwitch{SrcVlan: 100, DstVlan: 200}},
					{Data: sflow.ExtendedRouter{NextHop: utils.IPAddress{203, 0, 113, 1}, SrcMaskLen: 24, DstMaskLen: 22}},
					{Data: sflow.ExtendedGateway{
						NextHop:     utils.IPAddress{203, 0, 113, 1},
						AS:          64512,
						SrcAS:       64513,
						ASPath:      []uint32{64515, 64516, 64517},
						Communities: []uint32{100, 200, 300},
					}},
				},
			},
		},
	}
	msgs, err := ProcessMessageSFlowConfig(&packet, nil)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	msg := msgs[0].(*ProtoProducerMessage)
	msg.TimeReceivedNs = 1700000000000000000
	msg.TimeFlowStartNs = msg.TimeReceivedNs
	msg.TimeFlowEndNs = msg.TimeReceivedNs
	return msg
}

func TestMarshalBinary(t *testing.T) {
	msg := marshalTestMessage(t)

	var delimited bytes.Buffer
	_, err := protodelim.MarshalTo(&delimited, msg)
	require.NoError(t, err)
	out, err := msg.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, delimited.Bytes(), out)

	msg.skipDelimiter = true
	plain, err := proto.Marshal(msg)
	require.NoError(t, err)
	out, err = msg.MarshalBinary()
	require.NoError(t, err)
	require.Equal(t, plain, out)
}

func BenchmarkMarshalBinary(b *testing.B) {
	for _, skipDelimiter := range []bool{false, true} {
		name := "delimited"
		if skipDelimiter {
			name = "skip-delimiter"
		}
		b.Run(name, func(b *testing.B) {
			msg := marshalTestMessage(b)
			msg.skipDelimiter = skipDelimiter
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := msg.MarshalBinary(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
