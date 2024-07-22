package protoproducer

import (
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
	_, err := ParseEthernet2(&flowMessage, data, 0, 0)
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, uint32(0x86dd), flowMessage.Etype)
}

func TestProcessDot1Q2(t *testing.T) {
	dataStr := "00140800"
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := Parse8021Q2(&flowMessage, data, 0, 0)
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
	_, err := ParseMPLS2(&flowMessage, data, 0, 0)
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
	_, err := ParseIPv42(&flowMessage, data, 0, 0)
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
	_, err := ParseIPv62(&flowMessage, data, 0, 0)
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
	_, err := ParseIPv6HeaderFragment2(&flowMessage, data, 0, 0)
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
	_, err := ParseIPv6HeaderRouting2(&flowMessage, data, 0, 0)
	assert.NoError(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))
}

func TestProcessICMP2(t *testing.T) {
	dataStr := "01018cf7000627c4"

	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	_, err := ParseICMP2(&flowMessage, data, 0, 0)
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
	_, err := ParseICMPv62(&flowMessage, data, 0, 0)
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

	layers := []uint32{0, 6, 5, 2, 9}
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

	layers := []uint32{0, 2, 10, 1, 8}
	assert.Equal(t, len(layers), len(flowMessage.LayerStack))

	for i, layer := range layers {
		assert.Equal(t, layer, uint32(flowMessage.LayerStack[i]))
	}

	assert.Equal(t, uint32(0x86dd), flowMessage.Etype)
	assert.Equal(t, uint32(47), flowMessage.Proto)
	// todo: check addresses

}

// Legacy

func TestProcessEthernet(t *testing.T) {
	dataStr := "005300000001" + // src mac
		"005300000002" + // dst mac
		"86dd" + // etype
		"6000000004d83a40" + // ipv6
		"fd010000000000000000000000000001" + // src
		"fd010000000000000000000000000002" + // dst
		"8000f96508a4" // icmpv6
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	err := ParseEthernetHeader(&flowMessage, data, nil)
	assert.Nil(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b))

	assert.Equal(t, uint32(0x86dd), flowMessage.Etype)
	assert.Equal(t, uint32(58), flowMessage.Proto)
	assert.Equal(t, uint32(128), flowMessage.IcmpType)
}

func TestProcessIPv6Headers(t *testing.T) {
	dataStr := "6000000004d82c40" +
		"fd010000000000000000000000000001" + // src
		"fd010000000000000000000000000002" + // dst
		"3a000001a7882ea9" + // fragment header
		"8000f96508a4" // icmpv6
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	nextHeader, offset, err := ParseIPv6(0, &flowMessage, data)
	assert.Nil(t, err)
	assert.Equal(t, byte(44), nextHeader)
	nextHeader, offset, err = ParseIPv6Headers(nextHeader, offset, &flowMessage, data)
	assert.Nil(t, err)
	assert.Equal(t, byte(58), nextHeader)

	offset, err = ParseICMPv6(offset, &flowMessage, data)
	assert.Nil(t, err)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b), nextHeader, offset)

	assert.Equal(t, uint32(1), flowMessage.IpFlags)
	assert.Equal(t, uint32(64), flowMessage.IpTtl)
	assert.Equal(t, uint32(2810719913), flowMessage.FragmentId)
	assert.Equal(t, uint32(0), flowMessage.FragmentOffset)
	assert.Equal(t, uint32(128), flowMessage.IcmpType)
}

func TestProcessIPv4Fragment(t *testing.T) {
	dataStr := "450002245dd900b94001ffe1" +
		"c0a80101" + // src
		"c0a80102" + // dst
		"0809" // continued payload
	data, _ := hex.DecodeString(dataStr)
	var flowMessage ProtoProducerMessage
	nextHeader, offset, err := ParseIPv4(0, &flowMessage, data)
	assert.Nil(t, err)
	assert.Equal(t, byte(1), nextHeader)

	b, _ := json.Marshal(flowMessage.FlowMessage)
	t.Log(string(b), nextHeader, offset)

	assert.Equal(t, uint32(0), flowMessage.IpFlags)
	assert.Equal(t, uint32(64), flowMessage.IpTtl)
	assert.Equal(t, uint32(24025), flowMessage.FragmentId)
	assert.Equal(t, uint32(185), flowMessage.FragmentOffset)
}
