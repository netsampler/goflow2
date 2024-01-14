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
		"Etype":          EtypeRenderer,
		"Proto":          ProtoRenderer,
		"SrcNet":         NetworkRenderer,
		"DstNet":         NetworkRenderer,

		"icmp_name": ICMPRenderer,
	}

	etypeName = map[uint32]string{
		0x806:  "ARP",
		0x800:  "IPv4",
		0x86dd: "IPv6",
	}
	protoName = map[uint32]string{
		1:   "ICMP",
		6:   "TCP",
		17:  "UDP",
		58:  "ICMPv6",
		132: "SCTP",
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

func ProtoRenderer(msg *ProtoProducerMessage, fieldName string, data interface{}) interface{} {
	if dataC, ok := data.(uint32); ok {
		return protoName[dataC]
	} else if dataC, ok := data.(uint64); ok {
		return protoName[uint32(dataC)]
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
