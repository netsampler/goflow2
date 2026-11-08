package protoproducer

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	flowmessage "github.com/netsampler/goflow2/v3/pb"
)

// formatTestMessage returns a message with the fields an sFlow sample with a
// sampled header and extended switch, router and gateway records fills in,
// using the default formatter with a key.
func formatTestMessage(t testing.TB) *ProtoProducerMessage {
	t.Helper()
	cfg, err := (&ProducerConfig{
		Formatter: FormatterConfig{Key: []string{"sampler_address", "src_addr", "dst_addr"}},
	}).Compile()
	require.NoError(t, err)

	msg := &ProtoProducerMessage{formatter: cfg.GetFormatter()}
	msg.Type = flowmessage.FlowMessage_SFLOW_5
	msg.TimeReceivedNs = 1700000000000000000
	msg.TimeFlowStartNs = msg.TimeReceivedNs
	msg.TimeFlowEndNs = msg.TimeReceivedNs
	msg.SequenceNum = 2
	msg.SamplingRate = 2048
	msg.SamplerAddress = []byte{198, 51, 100, 1}
	msg.Bytes = 1518
	msg.Packets = 1
	msg.SrcAddr = []byte{10, 0, 0, 1}
	msg.DstAddr = []byte{10, 0, 0, 2}
	msg.Etype = 0x800
	msg.Proto = 6
	msg.SrcPort = 50000
	msg.DstPort = 443
	msg.InIf = 10
	msg.OutIf = 20
	msg.SrcMac = 0x005300000002
	msg.DstMac = 0x005300000001
	msg.SrcVlan = 100
	msg.DstVlan = 200
	msg.VlanId = 100
	msg.IpTtl = 64
	msg.TcpFlags = 0x18
	msg.IpFlags = 2
	msg.NextHop = []byte{203, 0, 113, 1}
	msg.BgpNextHop = []byte{203, 0, 113, 1}
	msg.SrcNet = 24
	msg.DstNet = 22
	msg.SrcAs = 64513
	msg.DstAs = 64517
	msg.NextHopAs = 64515
	msg.AsPath = []uint32{64515, 64516, 64517}
	msg.BgpCommunities = []uint32{100, 200, 300}
	msg.LayerStack = []flowmessage.FlowMessage_LayerStack{
		flowmessage.FlowMessage_Ethernet, flowmessage.FlowMessage_Dot1Q, flowmessage.FlowMessage_IPv4, flowmessage.FlowMessage_TCP,
	}
	msg.LayerSize = []uint32{14, 4, 20, 32}
	return msg
}

func TestFormatMessage(t *testing.T) {
	msg := formatTestMessage(t)

	out, err := msg.MarshalJSON()
	require.NoError(t, err)
	require.Equal(t, `{"type":"SFLOW_5","time_received_ns":1700000000000000000,"sequence_num":2,"sampling_rate":2048,`+
		`"sampler_address":"198.51.100.1","time_flow_start_ns":1700000000000000000,"time_flow_end_ns":1700000000000000000,`+
		`"bytes":1518,"packets":1,"src_addr":"10.0.0.1","dst_addr":"10.0.0.2","etype":"IPv4","proto":"TCP",`+
		`"src_port":50000,"dst_port":443,"in_if":10,"out_if":20,"src_mac":"00:53:00:00:00:02","dst_mac":"00:53:00:00:00:01",`+
		`"src_vlan":100,"dst_vlan":200,"vlan_id":100,"ip_tos":0,"forwarding_status":0,"ip_ttl":64,"ip_flags":2,"tcp_flags":24,`+
		`"icmp_type":0,"icmp_code":0,"ipv6_flow_label":0,"fragment_id":0,"fragment_offset":0,"src_as":64513,"dst_as":64517,`+
		`"next_hop":"203.0.113.1","next_hop_as":64515,"src_net":"10.0.0.0/24","dst_net":"10.0.0.0/22",`+
		`"bgp_next_hop":"203.0.113.1","bgp_communities":[100,200,300],"as_path":[64515,64516,64517],"mpls_ttl":[],`+
		`"mpls_label":[],"mpls_ip":[],"observation_domain_id":0,"observation_point_id":0,`+
		`"layer_stack":["Ethernet","Dot1Q","IPv4","TCP"],"layer_size":[14,4,20,32],"ipv6_routing_header_addresses":[],`+
		`"ipv6_routing_header_seg_left":0}`, string(out))

	require.Equal(t, "c8b15d98", hex.EncodeToString(msg.Key()))
}

func BenchmarkFormatMessage(b *testing.B) {
	msg := formatTestMessage(b)
	formats := []struct {
		name string
		fn   func() []byte
	}{
		{"json", func() []byte { out, _ := msg.MarshalJSON(); return out }},
		{"text", func() []byte { out, _ := msg.MarshalText(); return out }},
		{"key", msg.Key},
	}
	for _, format := range formats {
		b.Run(format.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if len(format.fn()) == 0 {
					b.Fatal("empty output")
				}
			}
		})
	}
}
