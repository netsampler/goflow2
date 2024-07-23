package protoproducer

import (
	"encoding/binary"
	"fmt"
)

type ParseConfig struct {
	Layer int
	Calls int
}

// BaseLayer indicates if the parser should map to the top-level fields of the protobuf
func (c *ParseConfig) BaseLayer() bool {
	return c.Calls == 0
}

// ParseResult contains information about the next
type ParseResult struct {
	NextParser Parser // Next parser to be called
	Size       int    // Size of the layer
}

type ParserInfo struct {
	Parser         Parser
	ConfigKeyList  []string
	CounterKeyList []string
}

// Parser is a function that maps various items of a layer to a ProtoProducerMessage
type Parser func(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error)

func NextParserEtype(etherType []byte) (Parser, error) {
	if len(etherType) != 2 {
		return nil, fmt.Errorf("wrong ether type")
	}
	switch {
	case etherType[0] == 0x19 && etherType[1] == 0x9e:
		return ParseEthernet2, nil // Transparent Ether Bridging (GRE)
	case etherType[0] == 0x88 && etherType[1] == 0x47:
		return ParseMPLS2, nil // MPLS
	case etherType[0] == 0x81 && etherType[1] == 0x0:
		return Parse8021Q2, nil // 802.1q
	case etherType[0] == 0x8 && etherType[1] == 0x0:
		return ParseIPv42, nil // IPv4
	case etherType[0] == 0x86 && etherType[1] == 0xdd:
		return ParseIPv62, nil // IPv6
	case etherType[0] == 0x8 && etherType[1] == 0x6:
		return nil, nil // ARP
	}
	return nil, nil
}

func NextProtocolParser(proto byte) (Parser, error) {
	switch {
	case proto == 1:
		return ParseICMP2, nil // ICMP
	case proto == 4:
		return ParseIPv42, nil // IPIP
	case proto == 6:
		return ParseTCP2, nil // TCP
	case proto == 17:
		return ParseUDP2, nil // UDP
	case proto == 41:
		return ParseIPv62, nil // IPv6IP
	case proto == 43:
		return ParseIPv6HeaderRouting2, nil // IPv6 EH Fragment
	case proto == 44:
		return ParseIPv6HeaderFragment2, nil // IPv6 EH Fragment
	case proto == 47:
		return ParseGRE2, nil // GRE
	case proto == 58:
		return ParseICMPv62, nil // ICMPv6
	case proto == 115:
		return nil, nil // L2TP
	}
	return nil, nil
}

func parserToStack(p Parser) {
}

func NextPortParser(srcPort, dstPort uint16) (Parser, error) {
	// Parser for GRE, Teredo, etc.
	// note: must depend on user configuration
	return nil, nil
}

func ParsePacket(flowMessage *ProtoProducerMessage, data []byte, config *SFlowMapper) (err error) {
	var offset int

	var nextParser Parser

	nextParser = ParseEthernet2 // initial parser
	//calls := make(map[interface{}]int) // indicates number of times the parser was called

	for nextParser != nil && len(data) >= offset { // check that a next parser exists and there is enough data to read
		// calls[nextParser]
		res, err := nextParser(flowMessage, data[offset:], ParseConfig{})
		//calls[nextParser] += 1
		if err != nil {
			return err
		}
		nextParser = res.NextParser
		offset += res.Size
	}
	return nil
}

func ParseEthernet2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 14 {
		return res, nil
	}

	res.Size = 14

	flowMessage.AddLayer("Ethernet")

	dstMac := binary.BigEndian.Uint64(append([]byte{0, 0}, data[0:6]...))
	srcMac := binary.BigEndian.Uint64(append([]byte{0, 0}, data[6:12]...))

	eType := data[12:14]

	if pc.Calls == 0 { // first time calling
		flowMessage.SrcMac = srcMac
		flowMessage.DstMac = dstMac
		flowMessage.Etype = uint32(binary.BigEndian.Uint16(eType))
	}
	// add to list of macs

	// get next parser
	res.NextParser, err = NextParserEtype(eType)

	return res, err
}

func Parse8021Q2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 4 {
		return res, nil
	}

	res.Size = 4

	flowMessage.AddLayer("Dot1Q")

	eType := data[2:4]

	if pc.Calls == 0 { // first time calling
		flowMessage.VlanId = uint32(binary.BigEndian.Uint16(data[0:2]))
		flowMessage.Etype = uint32(binary.BigEndian.Uint16(eType))
	}

	// get next parser
	res.NextParser, err = NextParserEtype(eType)

	return res, err
}

func ParseMPLS2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 4 {
		return res, nil
	}

	flowMessage.AddLayer("MPLS")

	var eType []byte
	var mplsLabel, mplsTtl []uint32

	iterateMpls := true
	var offset int
	for iterateMpls {
		if len(data) < offset+4 {
			// stop iterating mpls, not enough payload left
			break
		}
		label := binary.BigEndian.Uint32(append([]byte{0}, data[offset:offset+3]...)) >> 4
		//exp := data[offset+2] > 1
		bottom := data[offset+2] & 1
		ttl := data[offset+3]
		offset += 4

		if bottom == 1 || label <= 15 || offset > len(data) {

			if len(data) > offset {
				// peak at next byte
				if data[offset]&0xf0>>4 == 4 {
					eType = []byte{0x8, 0x0}
				} else if data[offset]&0xf0>>4 == 6 {
					eType = []byte{0x86, 0xdd}
				}
			}

			iterateMpls = false // stop iterating mpls, bottom of stack
		}

		mplsLabel = append(mplsLabel, label)
		mplsTtl = append(mplsTtl, uint32(ttl))
	}

	res.Size = offset

	if pc.Calls == 0 { // first time calling
		if len(eType) == 2 {
			flowMessage.Etype = uint32(binary.BigEndian.Uint16(eType))
		}

		flowMessage.MplsLabel = mplsLabel
		flowMessage.MplsTtl = mplsTtl
	}

	// get next parser
	if len(eType) == 2 {
		res.NextParser, err = NextParserEtype(eType)
	}

	return res, err
}

func ParseIPv42(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 20 {
		return res, nil
	}

	res.Size = 20

	flowMessage.AddLayer("IPv4")

	nextHeader := data[9]

	if pc.Calls == 0 { // first time calling
		flowMessage.SrcAddr = data[12:16]
		flowMessage.DstAddr = data[16:20]

		tos := data[1]
		ttl := data[8]

		flowMessage.IpTos = uint32(tos)
		flowMessage.IpTtl = uint32(ttl)

		identification := binary.BigEndian.Uint16(data[4:6])
		fragOffset := binary.BigEndian.Uint16(data[6:8]) // also includes flag

		flowMessage.FragmentId = uint32(identification)
		flowMessage.FragmentOffset = uint32(fragOffset) & 8191
		flowMessage.IpFlags = uint32(fragOffset) >> 13

		flowMessage.Proto = uint32(nextHeader)
	}

	// get next parser
	res.NextParser, err = NextProtocolParser(nextHeader)

	return res, err
}

func ParseIPv62(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 40 {
		return res, nil
	}

	res.Size = 40

	flowMessage.AddLayer("IPv6")

	nextHeader := data[6]

	if pc.Calls == 0 { // first time calling
		flowMessage.SrcAddr = data[8:24]
		flowMessage.DstAddr = data[24:40]

		tostmp := uint32(binary.BigEndian.Uint16(data[0:2]))
		tos := uint8(tostmp & 0x0ff0 >> 4)
		ttl := data[7]

		flowMessage.IpTos = uint32(tos)
		flowMessage.IpTtl = uint32(ttl)

		flowLabel := binary.BigEndian.Uint32(data[0:4])
		flowMessage.Ipv6FlowLabel = flowLabel & 0xFFFFF

		flowMessage.Proto = uint32(nextHeader)
	}

	// get next parser
	res.NextParser, err = NextProtocolParser(nextHeader)

	return res, err
}

func ParseIPv6HeaderFragment2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 8 {
		return res, nil
	}

	res.Size = 8

	// todo: add flowMessage.LayerStack

	nextHeader := data[0]

	if pc.Calls == 0 { // first time calling
		fragOffset := binary.BigEndian.Uint16(data[2:4]) // also includes flag
		identification := binary.BigEndian.Uint32(data[4:8])

		flowMessage.FragmentId = identification
		flowMessage.FragmentOffset = uint32(fragOffset) >> 3
		flowMessage.IpFlags = uint32(fragOffset) & 7
	}

	// get next parser
	res.NextParser, err = NextProtocolParser(nextHeader)

	return res, err
}

func ParseIPv6HeaderRouting2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 8 {
		return res, nil
	}

	nextHeader := data[0]
	length := data[1]

	res.Size = 8 + 8*int(length)

	// todo: add flowMessage.LayerStack

	if pc.Calls == 0 { // first time calling

		routingType := data[2]
		segLeft := data[3]

		if routingType == 4 { // Segment Routing

			lastEntry := data[4]
			var offset int
			var entry int

			for 8+offset < res.Size &&
				8+offset+16 <= len(data) &&
				entry <= int(lastEntry) {

				addr := data[8+offset : 8+offset+16]
				fmt.Printf("SRv6 IP %x (%d %d %d %d)\n", addr, offset, entry, lastEntry, segLeft)
				offset += 16
				entry++
			}
		}

	}

	// get next parser
	res.NextParser, err = NextProtocolParser(nextHeader)

	return res, err
}

func ParseTCP2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 20 {
		return res, nil
	}

	length := int(data[13]>>4) * 4

	res.Size = 20 + length

	flowMessage.AddLayer("TCP")

	if pc.Calls == 0 { // first time calling
		srcPort := binary.BigEndian.Uint16(data[0:2])
		dstPort := binary.BigEndian.Uint16(data[2:4])

		flowMessage.SrcPort = uint32(srcPort)
		flowMessage.DstPort = uint32(dstPort)

		tcpflags := data[13]
		flowMessage.TcpFlags = uint32(tcpflags)
	}

	return res, err
}

func ParseUDP2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 8 {
		return res, nil
	}

	res.Size = 8

	flowMessage.AddLayer("UDP")

	if pc.Calls == 0 { // first time calling
		srcPort := binary.BigEndian.Uint16(data[0:2])
		dstPort := binary.BigEndian.Uint16(data[2:4])

		flowMessage.SrcPort = uint32(srcPort)
		flowMessage.DstPort = uint32(dstPort)
	}

	return res, err
}

func ParseGRE2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 4 {
		return res, nil
	}

	res.Size = 4

	flowMessage.AddLayer("GRE")

	eType := data[2:4]

	// get next parser
	res.NextParser, err = NextParserEtype(eType)

	return res, err
}

func ParseICMP2(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 2 {
		return res, nil
	}

	res.Size = 8

	flowMessage.AddLayer("ICMP")

	if pc.Calls == 0 { // first time calling
		flowMessage.IcmpType = uint32(data[0])
		flowMessage.IcmpCode = uint32(data[1])
	}

	return res, err
}

func ParseICMPv62(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 2 {
		return res, nil
	}

	res.Size = 8

	flowMessage.AddLayer("ICMPv6")

	if pc.Calls == 0 { // first time calling
		flowMessage.IcmpType = uint32(data[0])
		flowMessage.IcmpCode = uint32(data[1])
	}

	return res, err
}

// Legacy decoders:

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
	if len(etherType) != 2 {
		return false
	}
	return etherType[0] == 0x88 && etherType[1] == 0x47
}

func Is8021Q(etherType []byte) bool {
	if len(etherType) != 2 {
		return false
	}
	return etherType[0] == 0x81 && etherType[1] == 0x0
}

func IsIPv4(etherType []byte) bool {
	if len(etherType) != 2 {
		return false
	}
	return etherType[0] == 0x8 && etherType[1] == 0x0
}

func IsIPv6(etherType []byte) bool {
	if len(etherType) != 2 {
		return false
	}
	return etherType[0] == 0x86 && etherType[1] == 0xdd
}

func IsARP(etherType []byte) bool {
	if len(etherType) != 2 {
		return false
	}
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

	if len(etherType) != 2 {
		return nil
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

	if len(etherType) >= 2 {
		flowMessage.Etype = uint32(binary.BigEndian.Uint16(etherType[0:2]))
	}
	flowMessage.Proto = uint32(nextHeader)

	return nil
}
