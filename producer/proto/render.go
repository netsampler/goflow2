package protoproducer

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"net/netip"
	"time"
)

type RenderFunc func(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{}

type RendererID string

const (
	RendererNone         RendererID = "none"
	RendererIP           RendererID = "ip"
	RendererMac          RendererID = "mac"
	RendererEtype        RendererID = "etype"
	RendererProto        RendererID = "proto"
	RendererType         RendererID = "type"
	RendererNetwork      RendererID = "network"
	RendererDateTime     RendererID = "datetime"
	RendererDateTimeNano RendererID = "datetimenano"
	RendererString       RendererID = "string"
)

var (
	renderers = map[RendererID]RenderFunc{
		RendererNone:         NilRenderer,
		RendererIP:           IPRenderer,
		RendererMac:          MacRenderer,
		RendererEtype:        EtypeRenderer,
		RendererProto:        ProtoRenderer,
		RendererDateTime:     DateTimeRenderer,
		RendererDateTimeNano: DateTimeNanoRenderer,
		RendererString:       StringRenderer,
	}

	defaultRenderers = map[string]RenderFunc{
		"SrcMac":         MacRenderer,
		"DstMac":         MacRenderer,
		"SrcAddr":        IPRenderer,
		"DstAddr":        IPRenderer,
		"SamplerAddress": IPRenderer,
		"NextHop":        IPRenderer,
		"BgpNextHop":     IPRenderer,
		"MplsLabelIp":    IPRenderer,
		"MplsIp":         IPRenderer,
		"Etype":          EtypeRenderer,
		"Proto":          ProtoRenderer,
		"SrcNet":         NetworkRenderer,
		"DstNet":         NetworkRenderer,

		"icmp_name": ICMPRenderer,

		"Ipv6RoutingHeaderAddresses": IPRenderer,
	}

	etypeName = map[uint32]string{
		0x806:  "ARP",
		0x800:  "IPv4",
		0x86dd: "IPv6",
	}
	protoName = map[uint32]string{
		0:   "HOPOPT",
		1:   "ICMP",
		2:   "IGMP",
		3:   "GGP",
		4:   "IPv4",
		5:   "ST",
		6:   "TCP",
		7:   "CBT",
		8:   "EGP",
		9:   "IGP",
		10:  "BBN-RCC-MON",
		11:  "NVP-II",
		12:  "PUP",
		13:  "ARGUS",
		14:  "EMCON",
		15:  "XNET",
		16:  "CHAOS",
		17:  "UDP",
		18:  "MUX",
		19:  "DCN-MEAS",
		20:  "HMP",
		21:  "PRM",
		22:  "XNS-IDP",
		23:  "TRUNK-1",
		24:  "TRUNK-2",
		25:  "LEAF-1",
		26:  "LEAF-2",
		27:  "RDP",
		28:  "IRTP",
		29:  "ISO-TP4",
		30:  "NETBLT",
		31:  "MFE-NSP",
		32:  "MERIT-INP",
		33:  "DCCP",
		34:  "3PC",
		35:  "IDPR",
		36:  "XTP",
		37:  "DDP",
		38:  "IDPR-CMTP",
		39:  "TP++",
		40:  "IL",
		41:  "IPv6",
		42:  "SDRP",
		43:  "IPv6-Route",
		44:  "IPv6-Frag",
		45:  "IDRP",
		46:  "RSVP",
		47:  "GRE",
		48:  "DSR",
		49:  "BNA",
		50:  "ESP",
		51:  "AH",
		52:  "I-NLSP",
		53:  "SWIPE",
		54:  "NARP",
		55:  "Min-IPv4",
		56:  "TLSP",
		57:  "SKIP",
		58:  "IPv6-ICMP",
		59:  "IPv6-NoNxt",
		60:  "IPv6-Opts",
		61:  "any-host-internal-protocol",
		62:  "CFTP",
		63:  "any-local-network",
		64:  "SAT-EXPAK",
		65:  "KRYPTOLAN",
		66:  "RVD",
		67:  "IPPC",
		68:  "any-distributed-file-system",
		69:  "SAT-MON",
		70:  "VISA",
		71:  "IPCV",
		72:  "CPNX",
		73:  "CPHB",
		74:  "WSN",
		75:  "PVP",
		76:  "BR-SAT-MON",
		77:  "SUN-ND",
		78:  "WB-MON",
		79:  "WB-EXPAK",
		80:  "ISO-IP",
		81:  "VMTP",
		82:  "SECURE-VMTP",
		83:  "VINES",
		84:  "IPTM",
		85:  "NSFNET-IGP",
		86:  "DGP",
		87:  "TCF",
		88:  "EIGRP",
		89:  "OSPFIGP",
		90:  "Sprite-RPC",
		91:  "LARP",
		92:  "MTP",
		93:  "AX.25",
		94:  "IPIP",
		95:  "MICP",
		96:  "SCC-SP",
		97:  "ETHERIP",
		98:  "ENCAP",
		99:  "any-private-encryption-scheme",
		100: "GMTP",
		101: "IFMP",
		102: "PNNI",
		103: "PIM",
		104: "ARIS",
		105: "SCPS",
		106: "QNX",
		107: "A/N",
		108: "IPComp",
		109: "SNP",
		110: "Compaq-Peer",
		111: "IPX-in-IP",
		112: "VRRP",
		113: "PGM",
		114: "any-0-hop-protocol",
		115: "L2TP",
		116: "DDX",
		117: "IATP",
		118: "STP",
		119: "SRP",
		120: "UTI",
		121: "SMP",
		122: "SM",
		123: "PTP",
		124: "ISIS over IPv4",
		125: "FIRE",
		126: "CRTP",
		127: "CRUDP",
		128: "SSCOPMCE",
		129: "IPLT",
		130: "SPS",
		131: "PIPE",
		132: "SCTP",
		133: "FC",
		134: "RSVP-E2E-IGNORE",
		135: "Mobility Header",
		136: "UDPLite",
		137: "MPLS-in-IP",
		138: "manet",
		139: "HIP",
		140: "Shim6",
		141: "WESP",
		142: "ROHC",
		143: "Ethernet",
		144: "AGGFRAG",
		145: "NSH",
	}
	icmpTypeName = map[uint32]string{
		0:  "EchoReply",
		3:  "DestinationUnreachable",
		8:  "Echo",
		9:  "RouterAdvertisement",
		10: "RouterSolicitation",
		11: "TimeExceeded",
	}
	icmp6TypeName = map[uint32]string{
		1:   "DestinationUnreachable",
		2:   "PacketTooBig",
		3:   "TimeExceeded",
		128: "EchoRequest",
		129: "EchoReply",
		133: "RouterSolicitation",
		134: "RouterAdvertisement",
	}
)

func NilRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataIf, ok := data.(interface {
		String() string
	}); ok {
		return dataIf.String()
	}
	if dataC, ok := data.([]byte); ok {
		return hex.EncodeToString(dataC)
	}
	return data
}

func StringRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataC, ok := data.([]byte); ok {
		return string(dataC)
	} else if dataC, ok := data.(string); ok {
		return string(dataC)
	} // maybe should support uint64?
	return NilRenderer(msg, fieldName, data)
}

func DateTimeRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataC, ok := data.(uint64); ok {
		ts := time.Unix(int64(dataC), 0).UTC()
		return ts.Format(time.RFC3339Nano)
	} else if dataC, ok := data.(int64); ok {
		ts := time.Unix(dataC, 0).UTC()
		return ts.Format(time.RFC3339Nano)
	} else if dataC, ok := data.(uint32); ok {
		ts := time.Unix(int64(dataC), 0).UTC()
		return ts.Format(time.RFC3339Nano)
	} else if dataC, ok := data.(int32); ok {
		ts := time.Unix(int64(dataC), 0).UTC()
		return ts.Format(time.RFC3339Nano)
	}
	return NilRenderer(msg, fieldName, data)
}

func DateTimeNanoRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataC, ok := data.(uint64); ok {
		ts := time.Unix(int64(dataC)/1e9, int64(dataC)%1e9).UTC()
		return ts.Format(time.RFC3339Nano)
	} else if dataC, ok := data.(int64); ok {
		ts := time.Unix(dataC/1e9, dataC%1e9).UTC()
		return ts.Format(time.RFC3339Nano)
	}
	return NilRenderer(msg, fieldName, data)
}

func MacRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataC, ok := data.(uint64); ok {
		var mac [8]byte
		binary.BigEndian.PutUint64(mac[:], dataC)
		return net.HardwareAddr(mac[2:]).String()
	}
	return NilRenderer(msg, fieldName, data)

}

func RenderIP(addr []byte) string {
	if addr == nil || (len(addr) != 4 && len(addr) != 16) {
		return ""
	}
	ip, _ := netip.AddrFromSlice(addr)
	return ip.String()
}

func IPRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataC, ok := data.([]byte); ok {
		return RenderIP(dataC)
	}
	return NilRenderer(msg, fieldName, data)
}

func EtypeRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataC, ok := data.(uint32); ok {
		return etypeName[dataC]
	} else if dataC, ok := data.(uint64); ok { // supports protobuf mapped fields
		return etypeName[uint32(dataC)]
	}
	return "unknown"
}

func ProtoName(protoNumber uint32) string {
	dataC, ok := protoName[protoNumber]
	if ok {
		return dataC
	} else if (protoNumber >= 146) && (protoNumber <= 252) {
		return "unassigned"
	} else if (protoNumber >= 253) && (protoNumber <= 254) {
		return "experimental"
	} else if protoNumber == 255 {
		return "reserved"
	}
	return "unknown"
}

func ProtoRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataC, ok := data.(uint32); ok {
		return ProtoName(dataC)
	} else if dataC, ok := data.(uint64); ok {
		return ProtoName(uint32(dataC))
	}
	return "unknown"
}

func NetworkRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	var addr netip.Addr
	if fieldName == "SrcNet" {
		addr, _ = netip.AddrFromSlice(msg.SrcAddr)
	} else if fieldName == "DstNet" {
		addr, _ = netip.AddrFromSlice(msg.DstAddr)
	}
	if dataC, ok := data.(uint32); ok {
		prefix, _ := addr.Prefix(int(dataC))
		return prefix.String()
	}
	return "unknown"
}

func IcmpCodeType(proto, icmpCode, icmpType uint32) string {
	if proto == 1 {
		return icmpTypeName[icmpType]
	} else if proto == 58 {
		return icmp6TypeName[icmpType]
	}
	return "unknown"
}

func ICMPRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	return IcmpCodeType(uint32(msg.Proto), uint32(msg.IcmpCode), uint32(msg.IcmpType))
}
