package common

import (
	"encoding/binary"
	"fmt"
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
	FORMAT_TYPE_BYTES
)

var (
	EtypeName = map[uint32]string{
		0x806:  "ARP",
		0x800:  "IPv4",
		0x86dd: "IPv6",
	}
	ProtoName = map[uint32]string{
		1:   "ICMP",
		6:   "TCP",
		17:  "UDP",
		58:  "ICMPv6",
		132: "SCTP",
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

	TextFields = map[string]int{
		"Type":           FORMAT_TYPE_STRING_FUNC,
		"SamplerAddress": FORMAT_TYPE_IP,
		"SrcAddr":        FORMAT_TYPE_IP,
		"DstAddr":        FORMAT_TYPE_IP,
		"SrcMac":         FORMAT_TYPE_MAC,
		"DstMac":         FORMAT_TYPE_MAC,
		"NextHop":        FORMAT_TYPE_IP,
		"MPLSLabelIP":    FORMAT_TYPE_IP,
	}

	RenderExtras = map[string]RenderExtraFunction{
		"EtypeName": RenderExtraFunctionEtypeName,
		"ProtoName": RenderExtraFunctionProtoName,
		"IcmpName":  RenderExtraFunctionIcmpName,
	}
)

/*
func AddTextField(name string, jtype int) {
	TextFields = append(TextFields, name)
	TextFieldsTypes = append(TextFieldsTypes, jtype)
}*/

type RenderExtraFunction func(interface{}) string

func RenderExtraFetchNumbers(msg interface{}, fields []string) []uint64 {
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

func RenderExtraFunctionEtypeName(msg interface{}) string {
	num := RenderExtraFetchNumbers(msg, []string{"Etype"})
	return EtypeName[uint32(num[0])]
}

func RenderExtraFunctionProtoName(msg interface{}) string {
	num := RenderExtraFetchNumbers(msg, []string{"Proto"})
	return ProtoName[uint32(num[0])]
}
func RenderExtraFunctionIcmpName(msg interface{}) string {
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

func FormatMessageReflectText(msg interface{}, ext string) string {
	return FormatMessageReflectCustom(msg, ext, "", " ", "=", false)
}

func FormatMessageReflectJSON(msg interface{}, ext string) string {
	return fmt.Sprintf("{%s}", FormatMessageReflectCustom(msg, ext, "\"", ",", ":", true))
}

func ExtractTag(name, original string, tag reflect.StructTag) string {
	lookup, ok := tag.Lookup(name)
	if !ok {
		return original
	}
	before, _, _ := strings.Cut(lookup, ",")
	return before
}

func FormatMessageReflectCustom(msg interface{}, ext, quotes, sep, sign string, null bool) string {
	customSelector := selector
	reMap := make(map[string]string)

	vfm := reflect.ValueOf(msg)
	vfm = reflect.Indirect(vfm)
	vft := vfm.Type()

	if len(customSelector) == 0 || selectorTag != "" {
		/*
			// we would need proto v2
			msgR := msg.ProtoReflect()
			customSelector = make([]string, msgR.Fields().Len())
			for i := 0; i<len(customSelector);i++ {
				customSelector[i] = msgR.Fields().Get(i).TextName()
			}*/

		customSelectorTmp := make([]string, vft.NumField())
		for i := 0; i < len(customSelectorTmp); i++ {
			field := vft.Field(i)
			if !field.IsExported() {
				continue
			}
			fieldName := field.Name
			if selectorTag != "" {
				fieldName = ExtractTag(selectorTag, field.Name, field.Tag)
				reMap[fieldName] = field.Name
			}
			customSelectorTmp[i] = fieldName

		}

		if len(customSelector) == 0 {
			customSelector = customSelectorTmp
		}
	}

	fstr := make([]string, len(customSelector))

	var i int

	for _, s := range customSelector {
		fieldName := s
		if fieldNameMap, ok := reMap[fieldName]; ok {
			fieldName = fieldNameMap
		}
		fieldValue := vfm.FieldByName(fieldName)
		// todo: replace s by json mapping of protobuf
		if fieldValue.IsValid() {

			if fieldType, ok := TextFields[fieldName]; ok {
				switch fieldType {
				case FORMAT_TYPE_STRING_FUNC:
					strMethod := fieldValue.MethodByName("String").Call([]reflect.Value{})
					fstr[i] = fmt.Sprintf("%s%s%s%s%q", quotes, s, quotes, sign, strMethod[0].String())
				case FORMAT_TYPE_STRING:
					fstr[i] = fmt.Sprintf("%s%s%s%s%q", quotes, s, quotes, sign, fieldValue.String())
				case FORMAT_TYPE_INTEGER:
					fstr[i] = fmt.Sprintf("%s%s%s%s%d", quotes, s, quotes, sign, fieldValue.Uint())
				case FORMAT_TYPE_IP:
					ip := fieldValue.Bytes()
					fstr[i] = fmt.Sprintf("%s%s%s%s%q", quotes, s, quotes, sign, RenderIP(ip))
				case FORMAT_TYPE_MAC:
					mac := make([]byte, 8)
					binary.BigEndian.PutUint64(mac, fieldValue.Uint())
					fstr[i] = fmt.Sprintf("%s%s%s%s%q", quotes, s, quotes, sign, net.HardwareAddr(mac[2:]).String())
				case FORMAT_TYPE_BYTES:
					fstr[i] = fmt.Sprintf("%s%s%s%s%.2x", quotes, s, quotes, sign, fieldValue.Bytes())
				default:
					if null {
						fstr[i] = fmt.Sprintf("%s%s%s%snull", quotes, s, quotes, sign)
					} else {

					}
				}
			} else if renderer, ok := RenderExtras[fieldName]; ok {
				fstr[i] = fmt.Sprintf("%s%s%s%s%q", quotes, s, quotes, sign, renderer(msg))
			} else {
				// handle specific types here
				switch fieldValue.Kind() {
				case reflect.String:
					fstr[i] = fmt.Sprintf("%s%s%s%s%q", quotes, s, quotes, sign, fieldValue.Interface())
				case reflect.Slice:
					c := fieldValue.Len()
					v := "["
					for i := 0; i < c; i++ {
						v += fmt.Sprintf("%v", fieldValue.Index(i).Interface())
						if i < c-1 {
							v += ","
						}
					}
					v += "]"
					fstr[i] = fmt.Sprintf("%s%s%s%s%s", quotes, s, quotes, sign, v)
				default:
					fstr[i] = fmt.Sprintf("%s%s%s%s%v", quotes, s, quotes, sign, fieldValue.Interface())
				}

			}
			i++
		}

	}
	fstr = fstr[0:i]

	return strings.Join(fstr, sep)
}
