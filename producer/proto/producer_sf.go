package protoproducer

import (
	"encoding/binary"

	"github.com/netsampler/goflow2/v2/decoders/sflow"
	flowmessage "github.com/netsampler/goflow2/v2/pb"
	"github.com/netsampler/goflow2/v2/producer"
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

func ParseEthernet(offset int, flowMessage *ProtoProducerMessage, data []byte) (etherType []byte, newOffset int, err error) {
	if len(data) >= offset+14 {
		etherType = data[offset+12 : offset+14]

		dstMac := binary.BigEndian.Uint64(append([]byte{0, 0}, data[offset+0:offset+6]...))
		srcMac := binary.BigEndian.Uint64(append([]byte{0, 0}, data[offset+6:offset+12]...))
		flowMessage.SrcMac = srcMac
		flowMessage.DstMac = dstMac

		offset += 14
	}
	return etherType, offset, err
}

func Parse8021Q(offset int, flowMessage *ProtoProducerMessage, data []byte) (etherType []byte, newOffset int, err error) {
	if len(data) >= offset+4 {
		flowMessage.VlanId = uint32(binary.BigEndian.Uint16(data[offset : offset+2]))
		etherType = data[offset+2 : offset+4]

		offset += 4
	}
	return etherType, offset, err
}

func ParseMPLS(offset int, flowMessage *ProtoProducerMessage, data []byte) (etherType []byte, newOffset int, err error) {
	var mplsLabel []uint32
	var mplsTtl []uint32

	iterateMpls := true
	for iterateMpls {
		if len(data) < offset+5 {
			// stop iterating mpls, not enough payload left
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
			iterateMpls = false // stop iterating mpls, bottom of stack
		}

		mplsLabel = append(mplsLabel, label)
		mplsTtl = append(mplsTtl, uint32(ttl))
	}
	// if multiple MPLS headers, will reset existing values
	flowMessage.MplsLabel = mplsLabel
	flowMessage.MplsTtl = mplsTtl
	return etherType, offset, err
}

func ParseIPv4(offset int, flowMessage *ProtoProducerMessage, data []byte) (nextHeader byte, newOffset int, err error) {
	if len(data) >= offset+20 {
		nextHeader = data[offset+9]
		flowMessage.SrcAddr = data[offset+12 : offset+16]
		flowMessage.DstAddr = data[offset+16 : offset+20]

		tos := data[offset+1]
		ttl := data[offset+8]

		flowMessage.IpTos = uint32(tos)
		flowMessage.IpTtl = uint32(ttl)

		identification := binary.BigEndian.Uint16(data[offset+4 : offset+6])
		fragOffset := binary.BigEndian.Uint16(data[offset+6 : offset+8]) // also includes flag

		flowMessage.FragmentId = uint32(identification)
		flowMessage.FragmentOffset = uint32(fragOffset) & 8191
		flowMessage.IpFlags = uint32(fragOffset) >> 13

		offset += 20
	}
	return nextHeader, offset, err
}

func ParseIPv6(offset int, flowMessage *ProtoProducerMessage, data []byte) (nextHeader byte, newOffset int, err error) {
	if len(data) >= offset+40 {
		nextHeader = data[offset+6]
		flowMessage.SrcAddr = data[offset+8 : offset+24]
		flowMessage.DstAddr = data[offset+24 : offset+40]

		tostmp := uint32(binary.BigEndian.Uint16(data[offset : offset+2]))
		tos := uint8(tostmp & 0x0ff0 >> 4)
		ttl := data[offset+7]

		flowMessage.IpTos = uint32(tos)
		flowMessage.IpTtl = uint32(ttl)

		flowLabel := binary.BigEndian.Uint32(data[offset : offset+4])
		flowMessage.Ipv6FlowLabel = flowLabel & 0xFFFFF

		offset += 40
	}
	return nextHeader, offset, err
}

func ParseIPv6Headers(nextHeader byte, offset int, flowMessage *ProtoProducerMessage, data []byte) (newNextHeader byte, newOffset int, err error) {
	for {
		if nextHeader == 44 && len(data) >= offset+8 {
			nextHeader = data[offset]

			fragOffset := binary.BigEndian.Uint16(data[offset+2 : offset+4]) // also includes flag
			identification := binary.BigEndian.Uint32(data[offset+4 : offset+8])

			flowMessage.FragmentId = identification
			flowMessage.FragmentOffset = uint32(fragOffset) >> 3
			flowMessage.IpFlags = uint32(fragOffset) & 7

			offset += 8
		} else {
			break
		}
	}
	return nextHeader, offset, err
}

func ParseTCP(offset int, flowMessage *ProtoProducerMessage, data []byte) (newOffset int, err error) {
	if len(data) >= offset+13 {
		srcPort := binary.BigEndian.Uint16(data[offset+0 : offset+2])
		dstPort := binary.BigEndian.Uint16(data[offset+2 : offset+4])

		flowMessage.SrcPort = uint32(srcPort)
		flowMessage.DstPort = uint32(dstPort)

		tcpflags := data[offset+13]
		flowMessage.TcpFlags = uint32(tcpflags)

		length := int(data[13]>>4) * 4

		offset += length
	}
	return offset, err
}

func ParseUDP(offset int, flowMessage *ProtoProducerMessage, data []byte) (newOffset int, err error) {
	if len(data) >= offset+4 {
		srcPort := binary.BigEndian.Uint16(data[offset+0 : offset+2])
		dstPort := binary.BigEndian.Uint16(data[offset+2 : offset+4])

		flowMessage.SrcPort = uint32(srcPort)
		flowMessage.DstPort = uint32(dstPort)

		offset += 8
	}
	return offset, err
}

func ParseICMP(offset int, flowMessage *ProtoProducerMessage, data []byte) (newOffset int, err error) {
	if len(data) >= offset+2 {
		flowMessage.IcmpType = uint32(data[offset+0])
		flowMessage.IcmpCode = uint32(data[offset+1])

		offset += 8
	}
	return offset, err
}

func ParseICMPv6(offset int, flowMessage *ProtoProducerMessage, data []byte) (newOffset int, err error) {
	if len(data) >= offset+2 {
		flowMessage.IcmpType = uint32(data[offset+0])
		flowMessage.IcmpCode = uint32(data[offset+1])

		offset += 8
	}
	return offset, err
}

func IsMPLS(etherType []byte) bool {
	return etherType[0] == 0x88 && etherType[1] == 0x47
}

func Is8021Q(etherType []byte) bool {
	return etherType[0] == 0x81 && etherType[1] == 0x0
}

func IsIPv4(etherType []byte) bool {
	return etherType[0] == 0x8 && etherType[1] == 0x0
}

func IsIPv6(etherType []byte) bool {
	return etherType[0] == 0x86 && etherType[1] == 0xdd
}

func IsARP(etherType []byte) bool {
	return etherType[0] == 0x8 && etherType[1] == 0x6
}

// Parses an entire stream consisting of multiple layers of protocols
// It picks the best field to map when multiple encapsulation of the same layer (eg: tunnels, extension headers, etc.)
func ParseEthernetHeader(flowMessage *ProtoProducerMessage, data []byte, config *SFlowMapper) (err error) {
	var nextHeader byte
	var offset int

	var etherType []byte

	for _, configLayer := range GetSFlowConfigLayer(config, "0") {
		extracted := GetBytes(data, offset+configLayer.Offset, configLayer.Length)
		if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
			return err
		}
	}

	if etherType, offset, err = ParseEthernet(offset, flowMessage, data); err != nil {
		return err
	}

	encap := true
	iterations := 0
	for encap && iterations <= 1 {
		encap = false

		if Is8021Q(etherType) { // VLAN 802.1Q
			if etherType, offset, err = Parse8021Q(offset, flowMessage, data); err != nil {
				return err
			}
		}

		if IsMPLS(etherType) { // MPLS
			if etherType, offset, err = ParseMPLS(offset, flowMessage, data); err != nil {
				return err
			}
		}

		for _, configLayer := range GetSFlowConfigLayer(config, "3") {
			extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
			if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
				return err
			}
		}

		if IsIPv4(etherType) { // IPv4
			prevOffset := offset
			if nextHeader, offset, err = ParseIPv4(offset, flowMessage, data); err != nil {
				return err
			}

			for _, configLayer := range GetSFlowConfigLayer(config, "ipv4") {
				extracted := GetBytes(data, prevOffset*8+configLayer.Offset, configLayer.Length)
				if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
					return err
				}
			}
		} else if IsIPv6(etherType) { // IPv6
			prevOffset := offset
			if nextHeader, offset, err = ParseIPv6(offset, flowMessage, data); err != nil {
				return err
			}
			if nextHeader, offset, err = ParseIPv6Headers(nextHeader, offset, flowMessage, data); err != nil {
				return err
			}

			for _, configLayer := range GetSFlowConfigLayer(config, "ipv6") {
				extracted := GetBytes(data, prevOffset*8+configLayer.Offset, configLayer.Length)
				if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
					return err
				}
			}
		} else if IsARP(etherType) { // ARP
			for _, configLayer := range GetSFlowConfigLayer(config, "arp") {
				extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
				if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
					return err
				}
			}
		}

		for _, configLayer := range GetSFlowConfigLayer(config, "4") {
			extracted := GetBytes(data, offset*8+configLayer.Offset, configLayer.Length)
			if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
				return err
			}
		}

		var appOffset int // keeps track of the user payload

		// Transport protocols
		if nextHeader == 17 || nextHeader == 6 || nextHeader == 1 || nextHeader == 58 {
			prevOffset := offset
			if flowMessage.FragmentOffset == 0 {
				if nextHeader == 17 { // UDP
					if offset, err = ParseUDP(offset, flowMessage, data); err != nil {
						return err
					}
					for _, configLayer := range GetSFlowConfigLayer(config, "udp") {
						extracted := GetBytes(data, prevOffset*8+configLayer.Offset, configLayer.Length)
						if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
							return err
						}
					}
				} else if nextHeader == 6 { // TCP
					if offset, err = ParseTCP(offset, flowMessage, data); err != nil {
						return err
					}
					for _, configLayer := range GetSFlowConfigLayer(config, "tcp") {
						extracted := GetBytes(data, prevOffset*8+configLayer.Offset, configLayer.Length)
						if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
							return err
						}
					}
				} else if nextHeader == 1 { // ICMP
					if offset, err = ParseICMP(offset, flowMessage, data); err != nil {
						return err
					}
					for _, configLayer := range GetSFlowConfigLayer(config, "icmp") {
						extracted := GetBytes(data, prevOffset*8+configLayer.Offset, configLayer.Length)
						if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
							return err
						}
					}
				} else if nextHeader == 58 { // ICMPv6
					if offset, err = ParseICMPv6(offset, flowMessage, data); err != nil {
						return err
					}
					for _, configLayer := range GetSFlowConfigLayer(config, "icmp6") {
						extracted := GetBytes(data, prevOffset*8+configLayer.Offset, configLayer.Length)
						if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
							return err
						}
					}
				}
			}
			appOffset = offset
		}

		// fetch data from the application/payload
		if appOffset > 0 {
			for _, configLayer := range GetSFlowConfigLayer(config, "7") {
				customOffset := appOffset*8 + configLayer.Offset - int(flowMessage.FragmentOffset)*8 // allows user to get data from a fragment as well
				// todo: check the calculation (might be off due to various header size)
				extracted := GetBytes(data, customOffset, configLayer.Length)
				if err := MapCustom(flowMessage, extracted, configLayer.MapConfigBase); err != nil {
					return err
				}
			}
		}

		iterations++
	}

	flowMessage.Etype = uint32(binary.BigEndian.Uint16(etherType[0:2]))
	flowMessage.Proto = uint32(nextHeader)

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
			if err := ParseSampledHeaderConfig(flowMessage, &recordData, config); err != nil { // todo: make function configurable
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

func SearchSFlowSamplesConfig(samples []interface{}, config *SFlowMapper) (flowMessageSet []producer.ProducerMessage, err error) {
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
func ProcessMessageSFlowConfig(packet *sflow.Packet, config *producerConfigMapped) (flowMessageSet []producer.ProducerMessage, err error) {
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
