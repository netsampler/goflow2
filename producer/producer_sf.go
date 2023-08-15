package producer

import (
	"encoding/binary"
	"errors"
	"net"

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

func ParseSampledHeader(flowMessage *flowmessage.FlowMessage, sampledHeader *sflow.SampledHeader) error {
	return ParseSampledHeaderConfig(flowMessage, sampledHeader, nil)
}

func ParseEthernetHeader(flowMessage *flowmessage.FlowMessage, data []byte, config *SFlowMapper) {
	var hasMpls bool
	var countMpls uint32
	var firstLabelMpls uint32
	var firstTtlMpls uint8
	var secondLabelMpls uint32
	var secondTtlMpls uint8
	var thirdLabelMpls uint32
	var thirdTtlMpls uint8
	var lastLabelMpls uint32
	var lastTtlMpls uint8

	var nextHeader byte
	var tcpflags byte
	srcIP := net.IP{}
	dstIP := net.IP{}
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

	for _, configLayer := range GetSFlowConfigLayer(config, 0) {
		extracted := GetBytes(data, configLayer.Offset, configLayer.Length)
		MapCustom(flowMessage, extracted, configLayer.Destination, configLayer.Endian)
	}

	etherType := data[12:14]

	dstMac = binary.BigEndian.Uint64(append([]byte{0, 0}, data[0:6]...))
	srcMac = binary.BigEndian.Uint64(append([]byte{0, 0}, data[6:12]...))
	(*flowMessage).SrcMac = srcMac
	(*flowMessage).DstMac = dstMac

	encap := true
	iterations := 0
	for encap && iterations <= 1 {
		encap = false

		if etherType[0] == 0x81 && etherType[1] == 0x0 { // VLAN 802.1Q
			(*flowMessage).VlanId = uint32(binary.BigEndian.Uint16(data[14:16]))
			offset += 4
			etherType = data[16:18]
		}

		if etherType[0] == 0x88 && etherType[1] == 0x47 { // MPLS
			iterateMpls := true
			hasMpls = true
			for iterateMpls {
				if len(data) < offset+5 {
					iterateMpls = false
					break
				}
				label := binary.BigEndian.Uint32(append([]byte{0}, data[offset:offset+3]...)) >> 4
				//exp := data[offset+2] > 1
				bottom := data[offset+2] & 1
				mplsTtl := data[offset+3]
				offset += 4

				if bottom == 1 || label <= 15 || offset > len(data) {
					if data[offset]&0xf0>>4 == 4 {
						etherType = []byte{0x8, 0x0}
					} else if data[offset]&0xf0>>4 == 6 {
						etherType = []byte{0x86, 0xdd}
					}
					iterateMpls = false
				}

				if countMpls == 0 {
					firstLabelMpls = label
					firstTtlMpls = mplsTtl
				} else if countMpls == 1 {
					secondLabelMpls = label
					secondTtlMpls = mplsTtl
				} else if countMpls == 2 {
					thirdLabelMpls = label
					thirdTtlMpls = mplsTtl
				} else {
					lastLabelMpls = label
					lastTtlMpls = mplsTtl
				}
				countMpls++
			}
		}

		for _, configLayer := range GetSFlowConfigLayer(config, 3) {
			extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
			MapCustom(flowMessage, extracted, configLayer.Destination, configLayer.Endian)
		}

		if etherType[0] == 0x8 && etherType[1] == 0x0 { // IPv4
			if len(data) >= offset+20 {
				nextHeader = data[offset+9]
				srcIP = data[offset+12 : offset+16]
				dstIP = data[offset+16 : offset+20]
				tos = data[offset+1]
				ttl = data[offset+8]

				identification = binary.BigEndian.Uint16(data[offset+4 : offset+6])
				fragOffset = binary.BigEndian.Uint16(data[offset+6:offset+8]) & 8191

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

				offset += 40

			}
		} else if etherType[0] == 0x8 && etherType[1] == 0x6 { // ARP
		} /*else {
			return errors.New(fmt.Sprintf("Unknown EtherType: %v\n", etherType))
		} */

		for _, configLayer := range GetSFlowConfigLayer(config, 4) {
			extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
			MapCustom(flowMessage, extracted, configLayer.Destination, configLayer.Endian)
		}

		appOffset := 0
		if len(data) >= offset+4 && (nextHeader == 17 || nextHeader == 6) && fragOffset&8191 == 0 {
			srcPort = binary.BigEndian.Uint16(data[offset+0 : offset+2])
			dstPort = binary.BigEndian.Uint16(data[offset+2 : offset+4])
		}

		if nextHeader == 17 {
			appOffset = 8
		}

		if len(data) > offset+13 && nextHeader == 6 {
			tcpflags = data[offset+13]

			appOffset = int(data[13]>>4) * 4
		}

		// ICMP and ICMPv6
		if len(data) >= offset+2 && (nextHeader == 1 || nextHeader == 58) {
			(*flowMessage).IcmpType = uint32(data[offset+0])
			(*flowMessage).IcmpCode = uint32(data[offset+1])
		}

		if appOffset > 0 {
			for _, configLayer := range GetSFlowConfigLayer(config, 7) {
				extracted := GetBytes(data, (offset+appOffset)*8+configLayer.Offset, configLayer.Length)
				MapCustom(flowMessage, extracted, configLayer.Destination, configLayer.Endian)
			}
		}

		iterations++
	}

	(*flowMessage).HasMpls = hasMpls
	(*flowMessage).MplsCount = countMpls
	(*flowMessage).Mpls_1Label = firstLabelMpls
	(*flowMessage).Mpls_1Ttl = uint32(firstTtlMpls)
	(*flowMessage).Mpls_2Label = secondLabelMpls
	(*flowMessage).Mpls_2Ttl = uint32(secondTtlMpls)
	(*flowMessage).Mpls_3Label = thirdLabelMpls
	(*flowMessage).Mpls_3Ttl = uint32(thirdTtlMpls)
	(*flowMessage).MplsLastLabel = lastLabelMpls
	(*flowMessage).MplsLastTtl = uint32(lastTtlMpls)

	(*flowMessage).Etype = uint32(binary.BigEndian.Uint16(etherType[0:2]))
	(*flowMessage).Ipv6FlowLabel = flowLabel & 0xFFFFF

	(*flowMessage).SrcPort = uint32(srcPort)
	(*flowMessage).DstPort = uint32(dstPort)

	(*flowMessage).SrcAddr = srcIP
	(*flowMessage).DstAddr = dstIP
	(*flowMessage).Proto = uint32(nextHeader)
	(*flowMessage).IpTos = uint32(tos)
	(*flowMessage).IpTtl = uint32(ttl)
	(*flowMessage).TcpFlags = uint32(tcpflags)

	(*flowMessage).FragmentId = uint32(identification)
	(*flowMessage).FragmentOffset = uint32(fragOffset)
}

func ParseSampledHeaderConfig(flowMessage *flowmessage.FlowMessage, sampledHeader *sflow.SampledHeader, config *SFlowMapper) error {
	data := (*sampledHeader).HeaderData
	switch (*sampledHeader).Protocol {
	case 1: // Ethernet
		ParseEthernetHeader(flowMessage, data, config)
	}
	return nil
}

func SearchSFlowSamples(samples []interface{}) []*flowmessage.FlowMessage {
	return SearchSFlowSamples(samples)
}

func SearchSFlowSamplesConfig(samples []interface{}, config *SFlowMapper) []*flowmessage.FlowMessage {
	var flowMessageSet []*flowmessage.FlowMessage

	for _, flowSample := range samples {
		var records []sflow.FlowRecord

		flowMessage := &flowmessage.FlowMessage{}
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

		ipNh := net.IP{}
		ipSrc := net.IP{}
		ipDst := net.IP{}
		flowMessage.Packets = 1
		for _, record := range records {
			switch recordData := record.Data.(type) {
			case sflow.SampledHeader:
				flowMessage.Bytes = uint64(recordData.FrameLength)
				ParseSampledHeaderConfig(flowMessage, &recordData, config)
			case sflow.SampledIPv4:
				ipSrc = recordData.Base.SrcIP
				ipDst = recordData.Base.DstIP
				flowMessage.SrcAddr = ipSrc
				flowMessage.DstAddr = ipDst
				flowMessage.Bytes = uint64(recordData.Base.Length)
				flowMessage.Proto = recordData.Base.Protocol
				flowMessage.SrcPort = recordData.Base.SrcPort
				flowMessage.DstPort = recordData.Base.DstPort
				flowMessage.IpTos = recordData.Tos
				flowMessage.Etype = 0x800
			case sflow.SampledIPv6:
				ipSrc = recordData.Base.SrcIP
				ipDst = recordData.Base.DstIP
				flowMessage.SrcAddr = ipSrc
				flowMessage.DstAddr = ipDst
				flowMessage.Bytes = uint64(recordData.Base.Length)
				flowMessage.Proto = recordData.Base.Protocol
				flowMessage.SrcPort = recordData.Base.SrcPort
				flowMessage.DstPort = recordData.Base.DstPort
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
		flowMessageSet = append(flowMessageSet, flowMessage)
	}
	return flowMessageSet
}

func ProcessMessageSFlow(msgDec interface{}) ([]*flowmessage.FlowMessage, error) {
	return ProcessMessageSFlowConfig(msgDec, nil)
}

func ProcessMessageSFlowConfig(msgDec interface{}, config *ProducerConfigMapped) ([]*flowmessage.FlowMessage, error) {
	switch packet := msgDec.(type) {
	case sflow.Packet:
		seqnum := packet.SequenceNumber
		var agent net.IP
		agent = packet.AgentIP

		var cfg *SFlowMapper
		if config != nil {
			cfg = config.SFlow
		}

		flowSamples := GetSFlowFlowSamples(&packet)
		flowMessageSet := SearchSFlowSamplesConfig(flowSamples, cfg)
		for _, fmsg := range flowMessageSet {
			fmsg.SamplerAddress = agent
			fmsg.SequenceNum = seqnum
		}

		return flowMessageSet, nil
	default:
		return []*flowmessage.FlowMessage{}, errors.New("Bad sFlow version")
	}
}
