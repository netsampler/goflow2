package protoproducer

import (
	"encoding/base64"

	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessEthernet2(t *testing.T) {
	dataStr := "005300000001" + // src mac
		"005300000002" + // dst mac
		"86dd" // etype
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseEthernet2(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, uint32(0x86dd), flowMessage.Etype)
}

func TestProcessDot1Q2(t *testing.T) {
	dataStr := "00140800"
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := Parse8021Q2(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, uint32(20), flowMessage.VlanId)
	assert.Equal(t, uint32(0x0800), flowMessage.Etype)
}

func TestProcessMPLS(t *testing.T) {
	dataStr := "000120ff" + // label 1
		"000101ff" // label 2
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseMPLS2(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, []uint32{18, 16}, flowMessage.MplsLabel)
	assert.Equal(t, []uint32{255, 255}, flowMessage.MplsTtl)
	//assert.Equal(t, uint32(0x800), flowMessage.Etype) // tested with next byte in whole packet
}

func TestProcessIPv42(t *testing.T) {
	dataStr := "45000064" +
		"abab" + // id
		"0000ff01" + // flag, ttl, proto
		"aaaa" + // csum
		"0a000001" + // src
		"0a000002" // dst
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseIPv42(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, []byte{10, 0, 0, 1}, flowMessage.SrcAddr)
	assert.Equal(t, []byte{10, 0, 0, 2}, flowMessage.DstAddr)
	assert.Equal(t, uint32(0xabab), flowMessage.FragmentId)
	assert.Equal(t, uint32(0xff), flowMessage.IpTtl)
	assert.Equal(t, uint32(1), flowMessage.Proto)
}

func TestProcessIPv62(t *testing.T) {
	dataStr := "6001010104d83a40" + // ipv6
		"fd010000000000000000000000000001" + // src
		"fd010000000000000000000000000002" // dst
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseIPv62(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, []byte{0xfd, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}, flowMessage.SrcAddr)
	assert.Equal(t, []byte{0xfd, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02}, flowMessage.DstAddr)
	assert.Equal(t, uint32(0x40), flowMessage.IpTtl)
	assert.Equal(t, uint32(0x3a), flowMessage.Proto)
	assert.Equal(t, uint32(0x010101), flowMessage.Ipv6FlowLabel)
}

func TestProcessIPv6HeaderFragment2(t *testing.T) {
	dataStr := "3a000001a7882ea9"

	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseIPv6HeaderFragment2(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, uint32(2810719913), flowMessage.FragmentId)
	assert.Equal(t, uint32(0), flowMessage.FragmentOffset)
}

func TestProcessIPv6HeaderRouting2(t *testing.T) {
	dataStr := "29060401020300102001baba0002e00200000000000000002001baba0001000000000000000000002001baba0003e0070000000000000000"

	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseIPv6HeaderRouting2(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))
}

func TestProcessICMP2(t *testing.T) {
	dataStr := "01018cf7000627c4"

	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseICMP2(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, uint32(1), flowMessage.IcmpType)
	assert.Equal(t, uint32(1), flowMessage.IcmpCode)
}

func TestProcessICMPv62(t *testing.T) {
	dataStr := "8080f96508a4"

	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseICMPv62(&flowMessage, data, ParseConfig{})
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, uint32(128), flowMessage.IcmpType)
	assert.Equal(t, uint32(128), flowMessage.IcmpCode)
}

func TestProcessPacketBase2(t *testing.T) {
	dataStr := "005300000001" + // src mac
		"005300000002" + // dst mac
		"8100" + // etype
		"00008847" + // 8021q
		"000120ff" + // mpls label 1
		"000101ff" + // mpls label 2
		"6000000004d83a40" + // ipv6
		"fd010000000000000000000000000001" + // src
		"fd010000000000000000000000000002" + // dst
		"8000f96508a4" // icmpv6

	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage

	err := ParsePacket(&flowMessage, data, nil)
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	layers := []uint32{0, 6, 5, 2, 8}
	assert.Equal(t, len(layers), len(flowMessage.LayerStack))

	for i, layer := range layers {
		assert.Equal(t, layer, uint32(flowMessage.LayerStack[i]))
	}

	assert.Equal(t, uint32(0x86dd), flowMessage.Etype)

}

func TestProcessPacketGRE2(t *testing.T) {
	dataStr := "005300000001" + // src mac
		"005300000002" + // dst mac
		"86dd" + // etype

		"6000000004d82f40" + // ipv6
		"fd010000000000000000000000000001" + // src
		"fd010000000000000000000000000002" + // dst

		"00000800" + // gre

		"45000064" + // ipv4
		"abab" + // id
		"0000ff01" + // flag, ttl, proto
		"aaaa" + // csum
		"0a000001" + // src
		"0a000002" + // dst

		"01018cf7000627c4" // icmp

	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	err := ParsePacket(&flowMessage, data, nil)
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	layers := []uint32{0, 2, 9, 1, 7}
	assert.Equal(t, len(layers), len(flowMessage.LayerStack))

	for i, layer := range layers {
		assert.Equal(t, layer, uint32(flowMessage.LayerStack[i]))
	}

	assert.Equal(t, uint32(0x86dd), flowMessage.Etype)
	assert.Equal(t, uint32(47), flowMessage.Proto)
	// todo: check addresses

}

type testProtoProducerMessage struct {
	ProtoProducerMessage
	t *testing.T
}

func (m *testProtoProducerMessage) MapCustom(key string, v []byte, cfg MapConfigBaseIf) error {
	m.t.Log("mapping", key, v)
	mc := MapConfigBase{
		Endianness: BigEndian,
		ProtoIndex: 999,
		ProtoType:  ProtoVarint,
		ProtoArray: false,
	}
	return m.ProtoProducerMessage.MapCustom(key, v, &mc)
}

func TestProcessPacketMapping(t *testing.T) {
	dataStr := "005300000001" + // src mac
		"005300000002" + // dst mac
		"0800" + // etype

		"45000064" + // ipv4
		"abab" + // id
		"0000ff11" + // flag, ttl, proto
		"aaaa" + // csum
		"0a000001" + // src
		"0a000002" + // dst

		// udp
		"ff00" + // src port
		"0035" + // dst port
		"0010" + // length
		"ffff" + // csum

		"0000000000000000" // payload

	config := SFlowProducerConfig{
		Mapping: []SFlowMapField{
			SFlowMapField{
				Layer:  "udp",
				Offset: 48,
				Length: 16,

				Destination: "csum",
			},
		},
	}
	configm := mapFieldsSFlow(config.Mapping)

	data, _ := hex.DecodeString(dataStr)
	flowMessage := testProtoProducerMessage{
		t: t,
	}

	err := ParsePacket(&flowMessage, data, configm)
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))
}

func TestProcessPacketMappingEncap(t *testing.T) {
	dataStr := "005300000001" + // src mac
		"005300000002" + // dst mac
		"86dd" + // etype

		"6001010104d82b40" + // ipv6
		"fd010000000000000000000000000001" + // src
		"fd010000000000000000000000000002" + // dst

		"04060401020300102001baba0002e00200000000000000002001baba0001000000000000000000002001baba0003e0070000000000000000" + // srv6

		"45000064" + // ipv4
		"abab" + // id
		"0000ff11" + // flag, ttl, proto
		"aaaa" + // csum
		"0a000001" + // src
		"0a000002" + // dst

		// udp
		"ff00" + // src port
		"0035" + // dst port
		"0010" + // length
		"ffff" + // csum

		"0000000000000000" // payload

	config := ProducerConfig{
		Formatter: FormatterConfig{
			Render: map[string]RendererID{
				"src_ip_encap": RendererIP,
				"dst_ip_encap": RendererIP,
			},
			Fields: []string{
				"src_ip_encap",
				"dst_ip_encap",
			},
			Protobuf: []ProtobufFormatterConfig{
				ProtobufFormatterConfig{
					Name:  "src_ip_encap",
					Index: 998,
					Type:  "string",
					Array: true,
				},
				ProtobufFormatterConfig{
					Name:  "dst_ip_encap",
					Index: 999,
					Type:  "string",
					Array: true,
				},
			},
		},
		SFlow: SFlowProducerConfig{
			Mapping: []SFlowMapField{
				SFlowMapField{
					Layer:        "ipv6",
					Offset:       64,
					Length:       128,
					Encapsulated: true,

					Destination: "src_ip_encap",
				},
				SFlowMapField{
					Layer:        "ipv6",
					Offset:       192,
					Length:       128,
					Encapsulated: true,

					Destination: "dst_ip_encap",
				},

				SFlowMapField{
					Layer:        "ipv4",
					Offset:       96,
					Length:       32,
					Encapsulated: true,

					Destination: "src_ip_encap",
				},
				SFlowMapField{
					Layer:        "ipv4",
					Offset:       128,
					Length:       32,
					Encapsulated: true,

					Destination: "dst_ip_encap",
				},
			},
		},
	}
	configm, _ := config.Compile()

	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	flowMessage.formatter = configm.GetFormatter()

	err := ParsePacket(&flowMessage, data, configm.GetPacketMapper())
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	flowMessage.skipDelimiter = true
	b, _ = flowMessage.MarshalBinary()
	t.Log(base64.StdEncoding.EncodeToString(b))

	b, _ = flowMessage.MarshalJSON()
	t.Log(string(b))
}
