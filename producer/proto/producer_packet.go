package protoproducer

import (
	"encoding/binary"
	"fmt"
)

var (
	parserNone = ParserInfo{
		nil,
		nil,
		100,
		9999,
	}
	parserPayload = ParserInfo{
		nil,
		[]string{"payload", "7"},
		100,
		9998,
	}

	parserEthernet = ParserInfo{
		nil, //ParseEthernet2,
		[]string{"ethernet", "2"},
		20,
		1,
	}
	parser8021Q = ParserInfo{
		nil, //Parse8021Q2,
		[]string{"dot1q"},
		25,
		2,
	}
	parserMPLS = ParserInfo{
		nil, //ParseMPLS2,
		[]string{"mpls"},
		25,
		3,
	}
	parserIPv4 = ParserInfo{
		nil, //ParseIPv42,
		[]string{"ipv4", "ip", "3"},
		30,
		4,
	}
	parserIPv6 = ParserInfo{
		nil, //ParseIPv62,
		[]string{"ipv6", "ip", "3"},
		30,
		5,
	}
	parserIPv6HeaderRouting = ParserInfo{
		nil, //ParseIPv6HeaderRouting2,
		[]string{"ipv6he_routing", "ipv6he"},
		30,
		7,
	}
	parserIPv6HeaderFragment = ParserInfo{
		nil, //ParseIPv6HeaderFragment2,
		[]string{"ipv6he_fragment", "ipv6he"},
		30,
		6,
	}
	parserTCP = ParserInfo{
		nil, //ParseTCP2,
		[]string{"tcp", "4"},
		40,
		8,
	}
	parserUDP = ParserInfo{
		nil, //ParseUDP2,
		[]string{"udp", "4"},
		40,
		9,
	}
	parserICMP = ParserInfo{
		nil, //ParseICMP2,
		[]string{"icmp"},
		70,
		10,
	}
	parserICMPv6 = ParserInfo{
		nil, //ParseICMPv62,
		[]string{"icmpv6"},
		70,
		11,
	}
	parserGRE = ParserInfo{
		nil, //ParseGRE2,
		[]string{"gre"},
		40,
		12,
	}
)

func init() {
	// necessary to set here otherwise initialization loop compilation error
	parserEthernet.Parser = ParseEthernet2
	parser8021Q.Parser = Parse8021Q2
	parserMPLS.Parser = ParseMPLS2
	parserIPv4.Parser = ParseIPv42
	parserIPv6.Parser = ParseIPv62
	parserIPv6HeaderRouting.Parser = ParseIPv6HeaderRouting2
	parserIPv6HeaderFragment.Parser = ParseIPv6HeaderFragment2
	parserTCP.Parser = ParseTCP2
	parserUDP.Parser = ParseUDP2
	parserICMP.Parser = ParseICMP2
	parserICMPv6.Parser = ParseICMPv62
	parserGRE.Parser = ParseGRE2
}

// Stores information about the current state of parsing
type ParseConfig struct {
	Layer        int  // absolute index of the layer
	Calls        int  // number of times the function was called (using parser index)
	LayerCall    int  // number of times a function in a layer (eg: Transport) was called (using layer index)
	Encapsulated bool // indicates if outside the typical mac-network-transport
}

// BaseLayer indicates if the parser should map to the top-level fields of the protobuf
func (c *ParseConfig) BaseLayer() bool {
	return !c.Encapsulated
}

// ParseResult contains information about the next
type ParseResult struct {
	NextParser ParserInfo // Next parser to be called
	Size       int        // Size of the layer
}

type ParserInfo struct {
	Parser        Parser
	ConfigKeyList []string // keys to match for custom parsing
	LayerIndex    int      // index to group
	ParserIndex   int      // unique parser index
}

// Parser is a function that maps various items of a layer to a ProtoProducerMessage
type Parser func(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error)

func NextParserEtype(etherType []byte) (ParserInfo, error) {
	info, err := innerNextParserEtype(etherType)
	etypeNum := uint16(etherType[0]<<8) | uint16(etherType[1])
	info.ConfigKeyList = append(info.ConfigKeyList, fmt.Sprintf("etype%d", etypeNum), fmt.Sprintf("etype0x%.4x", etypeNum))
	return info, err
}

func innerNextParserEtype(etherType []byte) (ParserInfo, error) {
	if len(etherType) != 2 {
		return parserNone, fmt.Errorf("wrong ether type")
	}
	switch {
	case etherType[0] == 0x19 && etherType[1] == 0x9e:
		return parserEthernet, nil // Transparent Ether Bridging (GRE)
	case etherType[0] == 0x88 && etherType[1] == 0x47:
		return parserMPLS, nil // MPLS
	case etherType[0] == 0x81 && etherType[1] == 0x0:
		return parser8021Q, nil // 802.1q
	case etherType[0] == 0x8 && etherType[1] == 0x0:
		return parserIPv4, nil // IPv4
	case etherType[0] == 0x86 && etherType[1] == 0xdd:
		return parserIPv6, nil // IPv6
	case etherType[0] == 0x8 && etherType[1] == 0x6:
		// ARP
	}
	return parserNone, nil
}

func NextProtocolParser(proto byte) (ParserInfo, error) {
	info, err := innerNextProtocolParser(proto)
	info.ConfigKeyList = append(info.ConfigKeyList, fmt.Sprintf("proto%d", proto))
	return info, err
}

func innerNextProtocolParser(proto byte) (ParserInfo, error) {
	switch {
	case proto == 1:
		return parserICMP, nil // ICMP
	case proto == 4:
		return parserIPv4, nil // IPIP
	case proto == 6:
		return parserTCP, nil // TCP
	case proto == 17:
		return parserUDP, nil // UDP
	case proto == 41:
		return parserIPv6, nil // IPv6IP
	case proto == 43:
		return parserIPv6HeaderRouting, nil // IPv6 EH Routing
	case proto == 44:
		return parserIPv6HeaderFragment, nil // IPv6 EH Fragment
	case proto == 47:
		return parserGRE, nil // GRE
	case proto == 58:
		return parserICMPv6, nil // ICMPv6
	case proto == 115:
		// L2TP
	}
	return parserNone, nil
}

func NextPortParser(srcPort, dstPort uint16) (ParserInfo, error) {
	// Parser for GRE, Teredo, etc.
	// note: must depend on user configuration
	return parserNone, nil
}

func ParsePacket(flowMessage ProtoProducerMessageIf, data []byte, config PacketMapper) (err error) {
	var offset int

	var nextParser ParserInfo
	var parseConfig ParseConfig

	nextParser = parserEthernet // initial parser
	callsLayer := make(map[int]int)
	calls := make(map[int]int)

	for nextParser.Parser != nil && len(data) >= offset { // check that a next parser exists and there is enough data to read
		parseConfig.Calls = calls[nextParser.ParserIndex]
		parseConfig.LayerCall = callsLayer[nextParser.LayerIndex]
		res, err := nextParser.Parser(flowMessage.GetFlowMessage(), data[offset:], parseConfig)
		parseConfig.Layer += 1
		if err != nil {
			return err
		}

		// Map custom fields
		for _, key := range nextParser.ConfigKeyList {
			if config != nil {
				layerIterator := config.Map(key)
				for {
					configLayer := layerIterator.Next()
					if configLayer == nil {
						break
					}
					extracted := GetBytes2(data, offset*8+configLayer.GetOffset(), configLayer.GetLength(), true)
					if err := flowMessage.MapCustom(key, extracted, configLayer); err != nil {
						return err
					}
				}
			}
		}

		if res.NextParser.LayerIndex < nextParser.LayerIndex {
			parseConfig.Encapsulated = true
		}

		nextParser = res.NextParser
		calls[nextParser.ParserIndex] += 1
		callsLayer[nextParser.LayerIndex] += 1

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

	if pc.BaseLayer() { // first time calling
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

	if pc.BaseLayer() { // first time calling
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

	if pc.BaseLayer() { // first time calling
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

	if pc.BaseLayer() { // first time calling
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

	if pc.BaseLayer() { // first time calling
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

	if pc.BaseLayer() { // first time calling
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

	if pc.BaseLayer() { // first time calling

		routingType := data[2]
		segLeft := data[3]

		flowMessage.Ipv6RoutingHeaderSegLeft = uint32(segLeft)

		if routingType == 4 { // Segment Routing

			lastEntry := data[4]
			var offset int
			var entry int

			for 8+offset < res.Size &&
				8+offset+16 <= len(data) &&
				entry <= int(lastEntry) {

				addr := data[8+offset : 8+offset+16]

				flowMessage.Ipv6RoutingHeaderAddresses = append(flowMessage.Ipv6RoutingHeaderAddresses, addr)

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

	if pc.BaseLayer() { // first time calling
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

	if pc.BaseLayer() { // first time calling
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
