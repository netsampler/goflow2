package common

import (
	"encoding/binary"
	"fmt"
	"github.com/golang/protobuf/proto"
	"net"
	"reflect"
	"strings"
)

const (
	FORMAT_TYPE_UNKNOWN = iota
	FORMAT_TYPE_STRING_FUNC
	FORMAT_TYPE_STRING
	FORMAT_TYPE_INTEGER
	FORMAT_TYPE_IP
	FORMAT_TYPE_MAC
)

var (
	EtypeName = map[uint32]string{
		0x806:  "ARP",
		0x800:  "IPv4",
		0x86dd: "IPv6",
	}
	ProtoName = map[uint32]string{
		1:  "ICMP",
		6:  "TCP",
		17: "UDP",
		58: "ICMPv6",
	}
	IcmpTypeName = map[uint32]string{
		0:  "EchoReply",
		3:  "DestinationUnreachable",
		8:  "Echo",
		9:  "RouterAdvertisement",
		10: "RouterSolicitation",
		11: "TimeExceeded",
	}
	Icmp6TypeName = map[uint32]string{
		1:   "DestinationUnreachable",
		2:   "PacketTooBig",
		3:   "TimeExceeded",
		128: "EchoRequest",
		129: "EchoReply",
		133: "RouterSolicitation",
		134: "RouterAdvertisement",
	}

	TextFields = []string{
		"Type",
		"TimeReceived",
		"SequenceNum",
		"SamplingRate",
		"SamplerAddress",
		"TimeFlowStart",
		"TimeFlowEnd",
		"Bytes",
		"Packets",
		"SrcAddr",
		"DstAddr",
		"Etype",
		"Proto",
		"SrcPort",
		"DstPort",
		"InIf",
		"OutIf",
		"SrcMac",
		"DstMac",
		"SrcVlan",
		"DstVlan",
		"VlanId",
		"IngressVrfID",
		"EgressVrfID",
		"IPTos",
		"ForwardingStatus",
		"IPTTL",
		"TCPFlags",
		"IcmpType",
		"IcmpCode",
		"IPv6FlowLabel",
		"FragmentId",
		"FragmentOffset",
		"BiFlowDirection",
		"SrcAS",
		"DstAS",
		"NextHop",
		"NextHopAS",
		"SrcNet",
		"DstNet",
	}
	TextFieldsTypes = []int{
		FORMAT_TYPE_STRING_FUNC,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_IP,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_IP,
		FORMAT_TYPE_IP,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_MAC,
		FORMAT_TYPE_MAC,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_IP,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
		FORMAT_TYPE_INTEGER,
	}
	RenderExtras = []string{
		"EtypeName",
		"ProtoName",
		"IcmpName",
	}
	RenderExtraCall = []RenderExtraFunction{
		RenderExtraFunctionEtypeName,
		RenderExtraFunctionProtoName,
		RenderExtraFunctionIcmpName,
	}
)

func AddTextField(name string, jtype int) {
	TextFields = append(TextFields, name)
	TextFieldsTypes = append(TextFieldsTypes, jtype)
}

type RenderExtraFunction func(proto.Message) string

func RenderExtraFetchNumbers(msg proto.Message, fields []string) []uint64 {
	vfm := reflect.ValueOf(msg)
	vfm = reflect.Indirect(vfm)

	values := make([]uint64, len(fields))
	for i, kf := range fields {
		fieldValue := vfm.FieldByName(kf)
		if fieldValue.IsValid() {
			values[i] = fieldValue.Uint()
		}
	}

	return values
}

func RenderExtraFunctionEtypeName(msg proto.Message) string {
	num := RenderExtraFetchNumbers(msg, []string{"Etype"})
	return EtypeName[uint32(num[0])]
}

func RenderExtraFunctionProtoName(msg proto.Message) string {
	num := RenderExtraFetchNumbers(msg, []string{"Proto"})
	return ProtoName[uint32(num[0])]
}
func RenderExtraFunctionIcmpName(msg proto.Message) string {
	num := RenderExtraFetchNumbers(msg, []string{"Proto", "IcmpCode", "IcmpType"})
	return IcmpCodeType(uint32(num[0]), uint32(num[1]), uint32(num[2]))
}

func IcmpCodeType(proto, icmpCode, icmpType uint32) string {
	if proto == 1 {
		return IcmpTypeName[icmpType]
	} else if proto == 58 {
		return Icmp6TypeName[icmpType]
	}
	return ""
}

func RenderIP(addr []byte) string {
	if addr == nil || (len(addr) != 4 && len(addr) != 16) {
		return ""
	}

	return net.IP(addr).String()
}

func FormatMessageReflectText(msg proto.Message, ext string) string {
	return FormatMessageReflectCustom(msg, ext, "", " ", "=", false)
}

func FormatMessageReflectJSON(msg proto.Message, ext string) string {
	return fmt.Sprintf("{%s}", FormatMessageReflectCustom(msg, ext, "\"", ",", ":", true))
}

func FormatMessageReflectCustom(msg proto.Message, ext, quotes, sep, sign string, null bool) string {
	fstr := make([]string, len(TextFields)+len(RenderExtras))

	vfm := reflect.ValueOf(msg)
	vfm = reflect.Indirect(vfm)

	for i, kf := range TextFields {
		fieldValue := vfm.FieldByName(kf)
		if fieldValue.IsValid() {

			switch TextFieldsTypes[i] {
			case FORMAT_TYPE_STRING_FUNC:
				strMethod := fieldValue.MethodByName("String").Call([]reflect.Value{})
				fstr[i] = fmt.Sprintf("%s%s%s%s%s%s%s", quotes, kf, quotes, sign, quotes, strMethod[0].String(), quotes)
			case FORMAT_TYPE_STRING:
				fstr[i] = fmt.Sprintf("%s%s%s%s%s%s%s", quotes, kf, quotes, sign, quotes, fieldValue.String(), quotes)
			case FORMAT_TYPE_INTEGER:
				fstr[i] = fmt.Sprintf("%s%s%s%s%d", quotes, kf, quotes, sign, fieldValue.Uint())
			case FORMAT_TYPE_IP:
				ip := fieldValue.Bytes()
				fstr[i] = fmt.Sprintf("%s%s%s%s%s%s%s", quotes, kf, quotes, sign, quotes, RenderIP(ip), quotes)
			case FORMAT_TYPE_MAC:
				mac := make([]byte, 8)
				binary.BigEndian.PutUint64(mac, fieldValue.Uint())
				fstr[i] = fmt.Sprintf("%s%s%s%s%s%s%s", quotes, kf, quotes, sign, quotes, net.HardwareAddr(mac[2:]).String(), quotes)
			default:
				if null {
					fstr[i] = fmt.Sprintf("%s%s%s:%s%s%s%snull", quotes, kf, quotes, sign)
				}
			}

		} else {
			if null {
				fstr[i] = fmt.Sprintf("\"%s\"%snull", kf, sign)
			}
		}
	}

	for i, e := range RenderExtras {
		fstr[i+len(TextFields)] = fmt.Sprintf("%s%s%s%s%s%s%s", quotes, e, quotes, sign, quotes, RenderExtraCall[i](msg), quotes)
	}

	return strings.Join(fstr, sep)
}
