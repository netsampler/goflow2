package protoproducer

import (
	"encoding/binary"
	"fmt"
	"sync"
)

// ParserEnvironment provides parser lookup helpers for packet decoding.
type ParserEnvironment interface {
	NextParserEtype(etherType []byte) (ParserInfo, error)
	NextParserProto(proto byte) (ParserInfo, error)
	NextParserPort(proto string, srcPort, dstPort uint16) (ParserInfo, error)
}

// RegPortDir indicates how port matching should be applied.
type RegPortDir string

var (
	// PortDirSrc matches on source port.
	PortDirSrc RegPortDir = "src"
	// PortDirDst matches on destination port.
	PortDirDst RegPortDir = "dst"
	// PortDirBoth matches on either source or destination port.
	PortDirBoth RegPortDir = "both"

	errParserEmpty = fmt.Errorf("parser is nil")

	parserNone = ParserInfo{
		nil,
		"none",
		nil,
		100,
		9999,
		false,
	}
	parserEthernet = ParserInfo{
		nil, //ParseEthernet2,
		"ethernet",
		[]string{"ethernet", "2"},
		20,
		1,
		false,
	}
	parser8021Q = ParserInfo{
		nil, //Parse8021Q2,
		"dot1q",
		[]string{"dot1q"},
		25,
		2,
		true,
	}
	parserMPLS = ParserInfo{
		nil, //ParseMPLS2,
		"mpls",
		[]string{"mpls"},
		25,
		3,
		true,
	}
	parserIPv4 = ParserInfo{
		nil, //ParseIPv42,
		"ipv4",
		[]string{"ipv4", "ip", "3"},
		30,
		4,
		false,
	}
	parserIPv6 = ParserInfo{
		nil, //ParseIPv62,
		"ipv6",
		[]string{"ipv6", "ip", "3"},
		30,
		5,
		false,
	}
	parserIPv6HeaderRouting = ParserInfo{
		nil, //ParseIPv6HeaderRouting2,
		"ipv6-route",
		[]string{"ipv6eh_routing", "ipv6-route", "ipv6eh"},
		35,
		7,
		false,
	}
	parserIPv6HeaderFragment = ParserInfo{
		nil, //ParseIPv6HeaderFragment2,
		"ipv6-frag",
		[]string{"ipv6eh_fragment", "ipv6-frag", "ipv6eh"},
		35,
		6,
		true,
	}
	parserTCP = ParserInfo{
		nil, //ParseTCP2,
		"tcp",
		[]string{"tcp", "4"},
		40,
		8,
		false,
	}
	parserUDP = ParserInfo{
		nil, //ParseUDP2,
		"udp",
		[]string{"udp", "4"},
		40,
		9,
		false,
	}
	parserICMP = ParserInfo{
		nil, //ParseICMP2,
		"icmp",
		[]string{"icmp"},
		70,
		10,
		false,
	}
	parserICMPv6 = ParserInfo{
		nil, //ParseICMPv62,
		"ipv6-icmp",
		[]string{"icmpv6", "ipv6-icmp"},
		70,
		11,
		false,
	}
	parserGRE = ParserInfo{
		nil, //ParseGRE2,
		"gre",
		[]string{"gre"},
		40,
		12,
		false,
	}
	parserTeredoDst = ParserInfo{
		nil, //ParseTeredoDst,
		"teredo-dst",
		[]string{"teredo-dst", "teredo"},
		40,
		13,
		false,
	}
	parserGeneve = ParserInfo{
		nil, //ParseTeredoDst,
		"geneve",
		[]string{"geneve"},
		40,
		14,
		false,
	}

	DefaultEnvironment *BaseParserEnvironment
)

func init() {
	// necessary to set here otherwise initialization loop compilation error
	parserEthernet.Parser = ParseEthernet
	parser8021Q.Parser = Parse8021Q
	parserMPLS.Parser = ParseMPLS
	parserIPv4.Parser = ParseIPv4
	parserIPv6.Parser = ParseIPv6
	parserIPv6HeaderRouting.Parser = ParseIPv6HeaderRouting
	parserIPv6HeaderFragment.Parser = ParseIPv6HeaderFragment
	parserTCP.Parser = ParseTCP
	parserUDP.Parser = ParseUDP
	parserICMP.Parser = ParseICMP
	parserICMPv6.Parser = ParseICMPv6
	parserGRE.Parser = ParseGRE
	parserTeredoDst.Parser = ParseTeredoDst
	parserGeneve.Parser = ParseGeneve

	DefaultEnvironment = NewBaseParserEnvironment()
}

// BaseParserEnvironment holds parser registrations and lookups.
type BaseParserEnvironment struct {
	nameToParser *sync.Map
	customEtype  *sync.Map
	customProto  *sync.Map
	customPort   *sync.Map
}

// NewBaseParserEnvironment creates a parser environment with defaults.
func NewBaseParserEnvironment() *BaseParserEnvironment {
	e := &BaseParserEnvironment{}
	e.nameToParser = &sync.Map{}
	e.customEtype = &sync.Map{}
	e.customProto = &sync.Map{}
	e.customPort = &sync.Map{}

	// Load initial parsers by name
	for _, p := range []ParserInfo{
		parserEthernet,
		parser8021Q,
		parserMPLS,
		parserIPv4,
		parserIPv6,
		parserIPv6HeaderRouting,
		parserIPv6HeaderFragment,
		parserTCP,
		parserUDP,
		parserICMP,
		parserICMPv6,
		parserGRE,
		parserTeredoDst,
		parserGeneve,
	} {
		e.nameToParser.Store(p.Name, p)
	}

	return e
}

// GetParser returns a parser by name.
func (e *BaseParserEnvironment) GetParser(name string) (info ParserInfo, ok bool) {
	parser, ok := e.nameToParser.Load(name)
	if ok {
		return parser.(ParserInfo), ok
	}
	return info, ok
}

// RegisterEtype registers a parser for a layer-2 EtherType.
func (e *BaseParserEnvironment) RegisterEtype(eType uint16, parser ParserInfo) error {
	if parser.Parser == nil {
		return errParserEmpty
	}
	e.customEtype.Store(eType, parser) // parser can be invoked to decode certain etypes
	return nil
}

// RegisterProto registers a parser for a layer-3 protocol.
func (e *BaseParserEnvironment) RegisterProto(proto byte, parser ParserInfo) error {
	if parser.Parser == nil {
		return errParserEmpty
	}
	e.customProto.Store(proto, parser) // parser can be invoked to decode certain protocols
	return nil
}

// RegisterPort registers a parser for layer-4 ports.
func (e *BaseParserEnvironment) RegisterPort(proto string, dir RegPortDir, port uint16, parser ParserInfo) error {
	if parser.Parser == nil {
		return errParserEmpty
	}
	switch dir {
	case PortDirBoth:
		e.customPort.Store(fmt.Sprintf("%s-src-%d", proto, port), parser)
		e.customPort.Store(fmt.Sprintf("%s-dst-%d", proto, port), parser)
	case PortDirSrc:
		e.customPort.Store(fmt.Sprintf("%s-src-%d", proto, port), parser)
	case PortDirDst:
		e.customPort.Store(fmt.Sprintf("%s-dst-%d", proto, port), parser)
	default:
		return fmt.Errorf("unknown direction %s", dir)
	}

	return nil
}

// NextParserEtype looks up the next parser by EtherType.
func (e *BaseParserEnvironment) NextParserEtype(etherType []byte) (ParserInfo, error) {
	info, err := e.innerNextParserEtype(etherType)
	etypeNum := uint16(etherType[0])<<8 | uint16(etherType[1])
	info.ConfigKeyList = append(info.ConfigKeyList, fmt.Sprintf("etype%d", etypeNum), fmt.Sprintf("etype0x%.4x", etypeNum))
	return info, err
}

func (e *BaseParserEnvironment) innerNextParserEtype(etherType []byte) (ParserInfo, error) {
	if len(etherType) != 2 {
		return parserNone, fmt.Errorf("wrong ether type")
	}

	eType := uint16(etherType[0])<<8 | uint16(etherType[1])
	if cParser, ok := e.customEtype.Load(eType); ok {
		return cParser.(ParserInfo), nil
	}

	switch eType {
	case 0x199e:
		return parserEthernet, nil // Transparent Ether Bridging (GRE)
	case 0x6558:
		return parserEthernet, nil // Transparent Ether Bridging (Geneve)
	case 0x8847:
		return parserMPLS, nil // MPLS
	case 0x8100:
		return parser8021Q, nil // 802.1q
	case 0x0800:
		return parserIPv4, nil // IPv4
	case 0x86dd:
		return parserIPv6, nil // IPv6
	case 0x0806:
		// ARP
	}
	return parserNone, nil
}

// NextParserProto looks up the next parser by protocol number.
func (e *BaseParserEnvironment) NextParserProto(proto byte) (ParserInfo, error) {
	info, err := e.innerNextParserProto(proto)
	info.ConfigKeyList = append(info.ConfigKeyList, fmt.Sprintf("proto%d", proto))
	return info, err
}

func (e *BaseParserEnvironment) innerNextParserProto(proto byte) (ParserInfo, error) {
	if cParser, ok := e.customProto.Load(proto); ok {
		return cParser.(ParserInfo), nil
	}

	switch proto {
	case 1:
		return parserICMP, nil // ICMP
	case 4:
		return parserIPv4, nil // IPIP
	case 6:
		return parserTCP, nil // TCP
	case 17:
		return parserUDP, nil // UDP
	case 41:
		return parserIPv6, nil // IPv6IP
	case 43:
		return parserIPv6HeaderRouting, nil // IPv6 EH Routing
	case 44:
		return parserIPv6HeaderFragment, nil // IPv6 EH Fragment
	case 47:
		return parserGRE, nil // GRE
	case 58:
		return parserICMPv6, nil // ICMPv6
	case 115:
		// L2TP
	}
	return parserNone, nil
}

// NextParserPort looks up the next parser by port match.
func (e *BaseParserEnvironment) NextParserPort(proto string, srcPort, dstPort uint16) (ParserInfo, error) {
	// Parser for GRE, Teredo, Geneve, etc.

	dir, info, err := e.innerNextParserPort(proto, srcPort, dstPort)
	// a custom parser must be present in order to expand the keys array
	switch dir {
	case 1:
		info.ConfigKeyList = append(info.ConfigKeyList, fmt.Sprintf("%s%d", proto, dstPort))
	case 2:
		info.ConfigKeyList = append(info.ConfigKeyList, fmt.Sprintf("%s%d", proto, srcPort))
	}
	return info, err
}

func (e *BaseParserEnvironment) innerNextParserPort(proto string, srcPort, dstPort uint16) (byte, ParserInfo, error) {
	if cParser, ok := e.customPort.Load(fmt.Sprintf("%s-dst-%d", proto, dstPort)); ok {
		return 1, cParser.(ParserInfo), nil
	}
	if cParser, ok := e.customPort.Load(fmt.Sprintf("%s-src-%d", proto, srcPort)); ok {
		return 2, cParser.(ParserInfo), nil
	}

	return 0, parserNone, nil
}

// ParsePacket parses a packet using the environment's parser chain.
func (e *BaseParserEnvironment) ParsePacket(flowMessage ProtoProducerMessageIf, data []byte) (err error) {
	return ParsePacket(flowMessage, data, nil, e)
}

// ParseConfig stores information about the current parsing state.
type ParseConfig struct {
	Environment  ParserEnvironment // parser configuration to customize chained calls
	Layer        int               // absolute index of the layer
	Calls        int               // number of times the function was called (using parser index)
	LayerCall    int               // number of times a function in a layer (eg: Transport) was called (using layer index)
	Encapsulated bool              // indicates if outside the typical mac-network-transport
}

// BaseLayer reports if the parser maps to the base layer.
func (c *ParseConfig) BaseLayer() bool {
	return !c.Encapsulated
}

// ParseResult contains information about the next parser and size.
type ParseResult struct {
	NextParser ParserInfo // Next parser to be called
	Size       int        // Size of the layer
}

// ParserInfo describes a parser and its metadata.
type ParserInfo struct {
	Parser        Parser
	Name          string
	ConfigKeyList []string // keys to match for custom parsing
	LayerIndex    int      // index to group
	ParserIndex   int      // unique parser index
	EncapSkip     bool     // indicates if should skip encapsulation calculations
}

// Parser maps items of a layer into a flow message.
type Parser func(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error)

// ParsePacket parses a packet using optional configuration and environment.
func ParsePacket(flowMessage ProtoProducerMessageIf, data []byte, config PacketLayerMapper, pe ParserEnvironment) (err error) {
	var offset int

	var nextParser ParserInfo
	var parseConfig ParseConfig

	if pe != nil {
		parseConfig.Environment = pe
	} else {
		parseConfig.Environment = DefaultEnvironment
	}

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
				for layerIterator != nil {
					configLayer := layerIterator.Next()
					if configLayer == nil {
						break
					}
					if configLayer.IsEncapsulated() != parseConfig.Encapsulated {
						continue
					}

					extracted := GetBytes(data, offset*8+configLayer.GetOffset(), configLayer.GetLength(), true)
					if err := flowMessage.MapCustom(key, extracted, configLayer); err != nil {
						return err
					}
				}
			}
		}

		fm := flowMessage.GetFlowMessage()
		fm.LayerSize = append(fm.LayerSize, uint32(res.Size))

		// compares the next layer index with current to determine if it's an encapsulation
		// IP over IP is the equals case
		// except if layer is skipping comparison (will be compared after). For instance IPv6 Fragment Header, dot1q and MPLS cannot trigger encap
		if !res.NextParser.EncapSkip && res.NextParser.LayerIndex <= nextParser.LayerIndex {
			parseConfig.Encapsulated = true
		}

		nextParser = res.NextParser
		calls[nextParser.ParserIndex] += 1
		callsLayer[nextParser.LayerIndex] += 1

		offset += res.Size
	}
	return nil
}

// ParseEthernet parses an Ethernet header.
func ParseEthernet(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
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

	if pc.Environment == nil {
		return res, err
	}
	// get next parser
	res.NextParser, err = pc.Environment.NextParserEtype(eType)

	return res, err
}

// Parse8021Q parses an 802.1Q VLAN header.
func Parse8021Q(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
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

	if pc.Environment == nil {
		return res, err
	}
	// get next parser
	res.NextParser, err = pc.Environment.NextParserEtype(eType)

	return res, err
}

// ParseMPLS parses an MPLS label stack.
func ParseMPLS(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
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
				switch data[offset] >> 4 {
				case 4:
					eType = []byte{0x8, 0x0}
				case 6:
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
		if pc.Environment == nil {
			return res, err
		}
		res.NextParser, err = pc.Environment.NextParserEtype(eType)
	}

	return res, err
}

// ParseIPv4 parses an IPv4 header.
func ParseIPv4(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
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

	if pc.Environment == nil {
		return res, err
	}
	// get next parser
	res.NextParser, err = pc.Environment.NextParserProto(nextHeader)

	return res, err
}

// ParseIPv6 parses an IPv6 header.
func ParseIPv6(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
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
	if pc.Environment == nil {
		return res, err
	}
	// get next parser
	res.NextParser, err = pc.Environment.NextParserProto(nextHeader)

	return res, err
}

// ParseIPv6HeaderFragment parses an IPv6 fragment header.
func ParseIPv6HeaderFragment(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 8 {
		return res, nil
	}

	res.Size = 8

	flowMessage.AddLayer("IPv6HeaderFragment")

	nextHeader := data[0]

	if pc.BaseLayer() { // first time calling
		fragOffset := binary.BigEndian.Uint16(data[2:4]) // also includes flag
		identification := binary.BigEndian.Uint32(data[4:8])

		flowMessage.FragmentId = identification
		flowMessage.FragmentOffset = uint32(fragOffset) >> 3
		flowMessage.IpFlags = uint32(fragOffset) & 7
	}
	if pc.Environment == nil {
		return res, err
	}
	// get next parser
	res.NextParser, err = pc.Environment.NextParserProto(nextHeader)

	return res, err
}

// ParseIPv6HeaderRouting parses an IPv6 routing header.
func ParseIPv6HeaderRouting(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 8 {
		return res, nil
	}

	nextHeader := data[0]
	length := data[1]

	res.Size = 8 + 8*int(length)

	flowMessage.AddLayer("IPv6HeaderRouting")

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
	if pc.Environment == nil {
		return res, err
	}
	// get next parser
	res.NextParser, err = pc.Environment.NextParserProto(nextHeader)

	return res, err
}

// ParseTCP parses a TCP header.
func ParseTCP(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 20 {
		return res, nil
	}

	length := int(data[13]>>4) * 4

	res.Size = 20 + length

	flowMessage.AddLayer("TCP")

	srcPort := binary.BigEndian.Uint16(data[0:2])
	dstPort := binary.BigEndian.Uint16(data[2:4])

	if pc.BaseLayer() { // first time calling
		flowMessage.SrcPort = uint32(srcPort)
		flowMessage.DstPort = uint32(dstPort)

		tcpflags := data[13]
		flowMessage.TcpFlags = uint32(tcpflags)
	}
	if pc.Environment == nil {
		return res, err
	}
	res.NextParser, err = pc.Environment.NextParserPort("tcp", srcPort, dstPort)

	return res, err
}

// ParseUDP parses a UDP header.
func ParseUDP(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 8 {
		return res, nil
	}

	res.Size = 8

	flowMessage.AddLayer("UDP")

	srcPort := binary.BigEndian.Uint16(data[0:2])
	dstPort := binary.BigEndian.Uint16(data[2:4])

	if pc.BaseLayer() { // first time calling
		flowMessage.SrcPort = uint32(srcPort)
		flowMessage.DstPort = uint32(dstPort)
	}
	if pc.Environment == nil {
		return res, err
	}
	res.NextParser, err = pc.Environment.NextParserPort("udp", srcPort, dstPort)

	return res, err
}

// ParseGRE parses a GRE header.
func ParseGRE(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 4 {
		return res, nil
	}

	res.Size = 4

	flowMessage.AddLayer("GRE")

	eType := data[2:4]
	if pc.Environment == nil {
		return res, err
	}
	// get next parser
	res.NextParser, err = pc.Environment.NextParserEtype(eType)

	return res, err
}

// ParseTeredoDst parses Teredo destination information.
func ParseTeredoDst(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	flowMessage.AddLayer("Teredo")

	// get next parser
	res.NextParser = parserIPv6

	return res, err
}

// ParseGeneve parses a Geneve header.
func ParseGeneve(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
	if len(data) < 8 {
		return res, nil
	}

	res.Size = int(data[0]&0x3f)*4 + 8

	flowMessage.AddLayer("Geneve")

	eType := data[2:4]
	if pc.Environment == nil {
		return res, err
	}
	// get next parser
	res.NextParser, err = pc.Environment.NextParserEtype(eType)

	return res, err
}

// ParseICMP parses an ICMP header.
func ParseICMP(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
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

// ParseICMPv6 parses an ICMPv6 header.
func ParseICMPv6(flowMessage *ProtoProducerMessage, data []byte, pc ParseConfig) (res ParseResult, err error) {
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
