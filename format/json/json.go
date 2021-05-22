package json

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/netsampler/goflow2/format"
	"github.com/netsampler/goflow2/format/protobuf"
	flowmessage "github.com/netsampler/goflow2/pb"
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

	JsonFields = []string{
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
	JsonFieldsTypes = []int{
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
	JsonExtras = []string{
		"EtypeName",
		"ProtoName",
		"IcmpName",
	}
	JsonExtraCall = []JsonExtraFunction{
		JsonExtraFunctionEtypeName,
		JsonExtraFunctionProtoName,
		JsonExtraFunctionIcmpName,
	}
)

func AddJSONField(name string, jtype int) {
	JsonFields = append(JsonFields, name)
	JsonFieldsTypes = append(JsonFieldsTypes, jtype)
}

type JsonExtraFunction func(proto.Message) string

func JsonExtraFetchNumbers(msg proto.Message, fields []string) []uint64 {
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

func JsonExtraFunctionEtypeName(msg proto.Message) string {
	num := JsonExtraFetchNumbers(msg, []string{"Etype"})
	return EtypeName[uint32(num[0])]
}
func JsonExtraFunctionProtoName(msg proto.Message) string {
	num := JsonExtraFetchNumbers(msg, []string{"Proto"})
	return ProtoName[uint32(num[0])]
}
func JsonExtraFunctionIcmpName(msg proto.Message) string {
	num := JsonExtraFetchNumbers(msg, []string{"Proto", "IcmpCode", "IcmpType"})
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

type JsonDriver struct {
	fieldsVar string
	fields    []string // Hashing fields
}

func RenderIP(addr []byte) string {
	if addr == nil || (len(addr) != 4 && len(addr) != 16) {
		return ""
	}

	return net.IP(addr).String()
}

func (d *JsonDriver) Prepare() error {
	return nil
}

func (d *JsonDriver) Init(context.Context) error {
	return protobuf.ManualInit()
}

func (d *JsonDriver) Format(data interface{}) ([]byte, []byte, error) {
	msg, ok := data.(proto.Message)
	if !ok {
		return nil, nil, fmt.Errorf("message is not protobuf")
	}

	key := protobuf.HashProtoLocal(msg)
	return []byte(key), []byte(FormatMessageReflect(msg, "")), nil
}

func FormatMessageReflect(msg proto.Message, ext string) string {
	fstr := make([]string, len(JsonFields)+len(JsonExtras))

	vfm := reflect.ValueOf(msg)
	vfm = reflect.Indirect(vfm)

	for i, kf := range JsonFields {
		fieldValue := vfm.FieldByName(kf)
		if fieldValue.IsValid() {

			switch JsonFieldsTypes[i] {
			case FORMAT_TYPE_STRING_FUNC:
				strMethod := fieldValue.MethodByName("String").Call([]reflect.Value{})
				fstr[i] = fmt.Sprintf("\"%s\":\"%s\"", kf, strMethod[0].String())
			case FORMAT_TYPE_STRING:
				fstr[i] = fmt.Sprintf("\"%s\":\"%s\"", kf, fieldValue.String())
			case FORMAT_TYPE_INTEGER:
				fstr[i] = fmt.Sprintf("\"%s\":%d", kf, fieldValue.Uint())
			case FORMAT_TYPE_IP:
				ip := fieldValue.Bytes()
				fstr[i] = fmt.Sprintf("\"%s\":\"%s\"", kf, RenderIP(ip))
			case FORMAT_TYPE_MAC:
				mac := make([]byte, 8)
				binary.BigEndian.PutUint64(mac, fieldValue.Uint())
				fstr[i] = fmt.Sprintf("\"%s\":\"%s\"", kf, net.HardwareAddr(mac[2:]).String())
			default:
				fstr[i] = fmt.Sprintf("\"%s\":null", kf)
			}

		} else {
			fstr[i] = fmt.Sprintf("\"%s\":null", kf)
		}
	}

	for i, e := range JsonExtras {
		fstr[i+len(JsonFields)] = fmt.Sprintf("\"%s\":\"%s\"", e, JsonExtraCall[i](msg))
	}

	return fmt.Sprintf("{%s}", strings.Join(fstr, ","))
}

func FormatMessage(msg *flowmessage.FlowMessage, ext string) string {
	srcmac := make([]byte, 8)
	dstmac := make([]byte, 8)
	binary.BigEndian.PutUint64(srcmac, msg.SrcMac)
	binary.BigEndian.PutUint64(dstmac, msg.DstMac)
	srcmac = srcmac[2:8]
	dstmac = dstmac[2:8]

	b := fmt.Sprintf(
		"{"+
			"\"Type\":\"%v\","+
			"\"TimeReceived\":%d,"+
			"\"SequenceNum\":%d,"+
			"\"SamplingRate\":%d,"+
			"\"SamplerAddress\":\"%v\","+
			"\"TimeFlowStart\":%d,"+
			"\"TimeFlowEnd\":%d,"+
			"\"Bytes\":%d,"+
			"\"Packets\":%d,"+
			"\"SrcAddr\":\"%v\","+
			"\"DstAddr\":\"%v\","+
			"\"Etype\":%d,"+
			"\"EtypeName\":\"%s\","+
			"\"Proto\":%d,"+
			"\"ProtoName\":\"%s\","+
			"\"SrcPort\":%d,"+
			"\"DstPort\":%d,"+
			"\"InIf\":%d,"+
			"\"OutIf\":%d,"+
			"\"SrcMac\":\"%v\","+
			"\"DstMac\":\"%v\","+
			"\"SrcVlan\":%d,"+
			"\"DstVlan\":%d,"+
			"\"VlanId\":%d,"+
			"\"IngressVrfID\":%d,"+
			"\"EgressVrfID\":%d,"+
			"\"IPTos\":%d,"+
			"\"ForwardingStatus\":%d,"+
			"\"IPTTL\":%d,"+
			"\"TCPFlags\":%d,"+
			"\"IcmpType\":%d,"+
			"\"IcmpCode\":%d,"+
			"\"IcmpName\":\"%s\","+
			"\"IPv6FlowLabel\":%d,"+
			"\"FragmentId\":%d,"+
			"\"FragmentOffset\":%d,"+
			"\"BiFlowDirection\":\"%v\","+
			"\"SrcAS\":%d,"+
			"\"DstAS\":%d,"+
			"\"NextHop\":\"%v\","+
			"\"NextHopAS\":%d,"+
			"\"SrcNet\":%d,"+
			"\"DstNet\":%d"+
			"%s}",
		msg.Type.String(),
		msg.TimeReceived,
		msg.SequenceNum,
		msg.SamplingRate,
		RenderIP(msg.SamplerAddress),
		msg.TimeFlowStart,
		msg.TimeFlowEnd,
		msg.Bytes,
		msg.Packets,
		RenderIP(msg.SrcAddr),
		RenderIP(msg.DstAddr),
		msg.Etype,
		EtypeName[msg.Etype],
		msg.Proto,
		ProtoName[msg.Proto],
		msg.SrcPort,
		msg.DstPort,
		msg.InIf,
		msg.OutIf,
		net.HardwareAddr(srcmac).String(),
		net.HardwareAddr(dstmac).String(),
		msg.SrcVlan,
		msg.DstVlan,
		msg.VlanId,
		msg.IngressVrfID,
		msg.EgressVrfID,
		msg.IPTos,
		msg.ForwardingStatus,
		msg.IPTTL,
		msg.TCPFlags,
		msg.IcmpType,
		msg.IcmpCode,
		IcmpCodeType(msg.Proto, msg.IcmpCode, msg.IcmpType),
		msg.IPv6FlowLabel,
		msg.FragmentId,
		msg.FragmentOffset,
		msg.BiFlowDirection,
		msg.SrcAS,
		msg.DstAS,
		RenderIP(msg.NextHop),
		msg.NextHopAS,
		msg.SrcNet,
		msg.DstNet,
		ext)

	return b
}

func init() {
	d := &JsonDriver{}
	format.RegisterFormatDriver("json", d)
}
