package producer

import (
	"encoding/binary"

	"github.com/netsampler/goflow2/decoders/sflow"
	flowmessage "github.com/netsampler/goflow2/pb"
)

func GetSFlowFlowSamples(packet *sflow.Packet) []interface{} {
	var flowSamples []interface{}
	for _, sample := range packet.Samples {
		switch sample.(type) {
		case sflow.FlowSample:
			flowSamples = append(flowSamples, sample)
		case sflow.ExpandedFlowSample:
			flowSamples = append(flowSamples, sample)
		}
	}
	return flowSamples
}

func ParseSampledHeader(flowMessage *ProtoProducerMessage, sampledHeader *sflow.SampledHeader) error {
	return ParseSampledHeaderConfig(flowMessage, sampledHeader, nil)
}

func ParseEthernetHeader(flowMessage *ProtoProducerMessage, data []byte, config *SFlowMapper) error {
	var mplsLabel []uint32
	var mplsTtl []uint32

	var nextHeader byte
	var tcpflags byte
	var srcIP, dstIP []byte
	offset := 14

	var srcMac uint64
	var dstMac uint64

	var tos byte
	var ttl byte
	var identification uint16
	var fragOffset uint16
	var flowLabel uint32

	var srcPort uint16
	var dstPort uint16

	for _, configLayer := range GetSFlowConfigLayer(config, "0") {
		extracted := GetBytes(data, configLayer.Offset, configLayer.Length)
		MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
	}

	etherType := data[12:14]

	dstMac = binary.BigEndian.Uint64(append([]byte{0, 0}, data[0:6]...))
	srcMac = binary.BigEndian.Uint64(append([]byte{0, 0}, data[6:12]...))
	flowMessage.SrcMac = srcMac
	flowMessage.DstMac = dstMac

	encap := true
	iterations := 0
	for encap && iterations <= 1 {
		encap = false

		if etherType[0] == 0x81 && etherType[1] == 0x0 { // VLAN 802.1Q
			flowMessage.VlanId = uint32(binary.BigEndian.Uint16(data[14:16]))
			offset += 4
			etherType = data[16:18]
		}

		if etherType[0] == 0x88 && etherType[1] == 0x47 { // MPLS
			iterateMpls := true
			for iterateMpls {
				if len(data) < offset+5 {
					iterateMpls = false
					break
				}
				label := binary.BigEndian.Uint32(append([]byte{0}, data[offset:offset+3]...)) >> 4
				//exp := data[offset+2] > 1
				bottom := data[offset+2] & 1
				ttl := data[offset+3]
				offset += 4

				if bottom == 1 || label <= 15 || offset > len(data) {
					if data[offset]&0xf0>>4 == 4 {
						etherType = []byte{0x8, 0x0}
					} else if data[offset]&0xf0>>4 == 6 {
						etherType = []byte{0x86, 0xdd}
					}
					iterateMpls = false
				}

				mplsLabel = append(mplsLabel, label)
				mplsTtl = append(mplsTtl, uint32(ttl))
			}
		}

		for _, configLayer := range GetSFlowConfigLayer(config, "3") {
			extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
			MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
		}

		if etherType[0] == 0x8 && etherType[1] == 0x0 { // IPv4
			if len(data) >= offset+20 {
				nextHeader = data[offset+9]
				srcIP = data[offset+12 : offset+16]
				dstIP = data[offset+16 : offset+20]
				tos = data[offset+1]
				ttl = data[offset+8]

				identification = binary.BigEndian.Uint16(data[offset+4 : offset+6])
				fragOffset = binary.BigEndian.Uint16(data[offset+6 : offset+8])

				for _, configLayer := range GetSFlowConfigLayer(config, "ipv4") {
					extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
					MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
				}

				offset += 20
			}
		} else if etherType[0] == 0x86 && etherType[1] == 0xdd { // IPv6
			if len(data) >= offset+40 {
				nextHeader = data[offset+6]
				srcIP = data[offset+8 : offset+24]
				dstIP = data[offset+24 : offset+40]

				tostmp := uint32(binary.BigEndian.Uint16(data[offset : offset+2]))
				tos = uint8(tostmp & 0x0ff0 >> 4)
				ttl = data[offset+7]

				flowLabel = binary.BigEndian.Uint32(data[offset : offset+4])

				for _, configLayer := range GetSFlowConfigLayer(config, "ipv6") {
					extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
					MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
				}

				offset += 40

			}
		} else if etherType[0] == 0x8 && etherType[1] == 0x6 { // ARP

		}

		for _, configLayer := range GetSFlowConfigLayer(config, "4") {
			extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
			MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
		}

		appOffset := 0
		if len(data) >= offset+4 && (nextHeader == 17 || nextHeader == 6) {
			srcPort = binary.BigEndian.Uint16(data[offset+0 : offset+2])
			dstPort = binary.BigEndian.Uint16(data[offset+2 : offset+4])
		}

		if nextHeader == 17 {
			appOffset = 8

			for _, configLayer := range GetSFlowConfigLayer(config, "udp") {
				extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
				MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
			}
		}

		if len(data) > offset+13 && nextHeader == 6 {
			tcpflags = data[offset+13]

			appOffset = int(data[13]>>4) * 4

			for _, configLayer := range GetSFlowConfigLayer(config, "tcp") {
				extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
				MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
			}
		}

		// ICMP and ICMPv6
		if len(data) >= offset+2 && (nextHeader == 1 || nextHeader == 58) {
			flowMessage.IcmpType = uint32(data[offset+0])
			flowMessage.IcmpCode = uint32(data[offset+1])

			if nextHeader == 1 {
				for _, configLayer := range GetSFlowConfigLayer(config, "icmp") {
					extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
					MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
				}
			} else if nextHeader == 58 {
				for _, configLayer := range GetSFlowConfigLayer(config, "icmp6") {
					extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
					MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
				}
			}
		}

		if appOffset > 0 {
			for _, configLayer := range GetSFlowConfigLayer(config, "7") {
				extracted := GetBytes(data, (offset+appOffset)*8+configLayer.Offset, configLayer.Length)
				MapCustom(flowMessage, extracted, configLayer.MapConfigBase)
			}
		}

		iterations++
	}

	flowMessage.MplsLabel = mplsLabel
	flowMessage.MplsTtl = mplsTtl

	flowMessage.Etype = uint32(binary.BigEndian.Uint16(etherType[0:2]))
	flowMessage.Ipv6FlowLabel = flowLabel & 0xFFFFF

	flowMessage.SrcPort = uint32(srcPort)
	flowMessage.DstPort = uint32(dstPort)

	flowMessage.SrcAddr = srcIP
	flowMessage.DstAddr = dstIP
	flowMessage.Proto = uint32(nextHeader)
	flowMessage.IpTos = uint32(tos)
	flowMessage.IpTtl = uint32(ttl)
	flowMessage.TcpFlags = uint32(tcpflags)

	flowMessage.FragmentId = uint32(identification)
	flowMessage.FragmentOffset = uint32(fragOffset)

	return nil
}

func ParseSampledHeaderConfig(flowMessage *ProtoProducerMessage, sampledHeader *sflow.SampledHeader, config *SFlowMapper) error {
	data := (*sampledHeader).HeaderData
	switch (*sampledHeader).Protocol {
	case 1: // Ethernet
		if err := ParseEthernetHeader(flowMessage, data, config); err != nil {
			return err
		}
	}
	return nil
}

func SearchSFlowSamples(samples []interface{}) []ProducerMessage {
	return SearchSFlowSamples(samples)
}

func SearchSFlowSampleConfig(flowMessage *ProtoProducerMessage, flowSample interface{}, config *SFlowMapper) error {
	var records []sflow.FlowRecord
	flowMessage.Type = flowmessage.FlowMessage_SFLOW_5

	switch flowSample := flowSample.(type) {
	case sflow.FlowSample:
		records = flowSample.Records
		flowMessage.SamplingRate = uint64(flowSample.SamplingRate)
		flowMessage.InIf = flowSample.Input
		flowMessage.OutIf = flowSample.Output
	case sflow.ExpandedFlowSample:
		records = flowSample.Records
		flowMessage.SamplingRate = uint64(flowSample.SamplingRate)
		flowMessage.InIf = flowSample.InputIfValue
		flowMessage.OutIf = flowSample.OutputIfValue
	}

	var ipNh, ipSrc, ipDst []byte
	flowMessage.Packets = 1
	for _, record := range records {
		switch recordData := record.Data.(type) {
		case sflow.SampledHeader:
			flowMessage.Bytes = uint64(recordData.FrameLength)
			if err := ParseSampledHeaderConfig(flowMessage, &recordData, config); err != nil {
				return err
			}
		case sflow.SampledIPv4:
			ipSrc = recordData.SrcIP
			ipDst = recordData.DstIP
			flowMessage.SrcAddr = ipSrc
			flowMessage.DstAddr = ipDst
			flowMessage.Bytes = uint64(recordData.Length)
			flowMessage.Proto = recordData.Protocol
			flowMessage.SrcPort = recordData.SrcPort
			flowMessage.DstPort = recordData.DstPort
			flowMessage.IpTos = recordData.Tos
			flowMessage.Etype = 0x800
		case sflow.SampledIPv6:
			ipSrc = recordData.SrcIP
			ipDst = recordData.DstIP
			flowMessage.SrcAddr = ipSrc
			flowMessage.DstAddr = ipDst
			flowMessage.Bytes = uint64(recordData.Length)
			flowMessage.Proto = recordData.Protocol
			flowMessage.SrcPort = recordData.SrcPort
			flowMessage.DstPort = recordData.DstPort
			flowMessage.IpTos = recordData.Priority
			flowMessage.Etype = 0x86dd
		case sflow.ExtendedRouter:
			ipNh = recordData.NextHop
			flowMessage.NextHop = ipNh
			flowMessage.SrcNet = recordData.SrcMaskLen
			flowMessage.DstNet = recordData.DstMaskLen
		case sflow.ExtendedGateway:
			ipNh = recordData.NextHop
			flowMessage.BgpNextHop = ipNh
			flowMessage.BgpCommunities = recordData.Communities
			flowMessage.AsPath = recordData.ASPath
			if len(recordData.ASPath) > 0 {
				flowMessage.DstAs = recordData.ASPath[len(recordData.ASPath)-1]
				flowMessage.NextHopAs = recordData.ASPath[0]
			} else {
				flowMessage.DstAs = recordData.AS
			}
			if recordData.SrcAS > 0 {
				flowMessage.SrcAs = recordData.SrcAS
			} else {
				flowMessage.SrcAs = recordData.AS
			}
		case sflow.ExtendedSwitch:
			flowMessage.SrcVlan = recordData.SrcVlan
			flowMessage.DstVlan = recordData.DstVlan
		}
	}
	return nil

}

func SearchSFlowSamplesConfig(samples []interface{}, config *SFlowMapper) ([]ProducerMessage, error) {
	var flowMessageSet []ProducerMessage

	for _, flowSample := range samples {
		fmsg := protoMessagePool.Get().(*ProtoProducerMessage)
		fmsg.Reset()
		if err := SearchSFlowSampleConfig(fmsg, flowSample, config); err != nil {
			return nil, err
		}
		flowMessageSet = append(flowMessageSet, fmsg)
	}
	return flowMessageSet, nil
}

// Converts an sFlow message
func ProcessMessageSFlowConfig(packet *sflow.Packet, config *producerConfigMapped) (flowMessageSet []ProducerMessage, err error) {
	seqnum := packet.SequenceNumber
	agent := packet.AgentIP

	var cfg *SFlowMapper
	if config != nil {
		cfg = config.SFlow
	}

	flowSamples := GetSFlowFlowSamples(packet)
	flowMessageSet, err = SearchSFlowSamplesConfig(flowSamples, cfg)
	if err != nil {
		return flowMessageSet, err
	}
	for _, msg := range flowMessageSet {
		fmsg, ok := msg.(*ProtoProducerMessage)
		if !ok {
			continue
		}
		fmsg.SamplerAddress = agent
		fmsg.SequenceNum = seqnum
	}

	return flowMessageSet, nil
}
