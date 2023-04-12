package producer

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protowire"

	flowmessage "github.com/netsampler/goflow2/pb"
)

type ProtoProducerMessage struct {
	flowmessage.FlowMessage

	customSelector []string
	selectorTag    string

	formatter *FormatterConfigMapper
}

var protoMessagePool = sync.Pool{
	New: func() any {
		return &ProtoProducerMessage{}
	},
}

func (m *ProtoProducerMessage) String() string {
	m.customSelector = []string{"Type", "SrcAddr"}
	return m.FormatMessageReflectText(m, "")
}

func (m *ProtoProducerMessage) Key() []byte {
	m.customSelector = []string{"Type", "SrcAddr"}
	return []byte(m.FormatMessageReflectText(m, ""))
}

func (m *ProtoProducerMessage) MarshalJSON() ([]byte, error) {
	return []byte(m.FormatMessageReflectJSON(m, "")), nil
}

// -----

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
		"type":            FORMAT_TYPE_STRING_FUNC,
		"sampler_address": FORMAT_TYPE_IP,
		"src_addr":        FORMAT_TYPE_IP,
		"dst_addr":        FORMAT_TYPE_IP,
		"src_mac":         FORMAT_TYPE_MAC,
		"dst_mac":         FORMAT_TYPE_MAC,
		"next_hop":        FORMAT_TYPE_IP,
		"mpls_label_ip":   FORMAT_TYPE_IP,
	}

	RenderExtras = map[string]RenderExtraFunction{
		"etype_name": RenderExtraFunctionEtypeName,
		"proto_name": RenderExtraFunctionProtoName,
		"icmp_name":  RenderExtraFunctionIcmpName,
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

func (m *ProtoProducerMessage) FormatMessageReflectText(msg interface{}, ext string) string {
	return m.FormatMessageReflectCustom(msg, ext, "", " ", "=", false)
}

func (m *ProtoProducerMessage) FormatMessageReflectJSON(msg interface{}, ext string) string {
	return fmt.Sprintf("{%s}", m.FormatMessageReflectCustom(msg, ext, "\"", ",", ":", true))
}

func ExtractTag(name, original string, tag reflect.StructTag) string {
	lookup, ok := tag.Lookup(name)
	if !ok {
		return original
	}
	before, _, _ := strings.Cut(lookup, ",")
	return before
}

func (m *ProtoProducerMessage) FormatMessageReflectCustom(msg interface{}, ext, quotes, sep, sign string, null bool) string {
	//customSelector := selector
	reMap := make(map[string]string)

	vfm := reflect.ValueOf(msg)
	vfm = reflect.Indirect(vfm)

	var i int
	fstr := make([]string, len(m.formatter.fields)) // todo: reuse with pool

	unkMap := make(map[string]interface{})
	if flowMessage, ok := msg.(*ProtoProducerMessage); ok {
		fmr := flowMessage.ProtoReflect()
		unk := fmr.GetUnknown()
		var offset int
		for offset < len(unk) {
			num, dataType, length := protowire.ConsumeTag(unk[offset:])
			offset += length
			length = protowire.ConsumeFieldValue(num, dataType, unk[offset:])
			data := unk[offset : offset+length]
			offset += length

			// we check if the index is listed in the config
			if pbField, ok := m.formatter.numToPb[int32(num)]; ok {

				var dest interface{}
				var value interface{}
				if dataType == protowire.VarintType {
					v, _ := protowire.ConsumeVarint(data)
					value = v
				} else if dataType == protowire.BytesType {
					v, _ := protowire.ConsumeString(data)
					value = hex.EncodeToString([]byte(v))
				} else {
					continue
				}
				if pbField.Array {
					var destSlice []interface{}
					if dest, ok := unkMap[pbField.Name]; !ok {
						destSlice = make([]interface{}, 0)
					} else {
						destSlice = dest.([]interface{})
					}
					destSlice = append(destSlice, value)
					dest = destSlice
				} else {
					dest = value
				}

				unkMap[pbField.Name] = dest

			}
		}
	}

	for _, s := range m.formatter.fields {
		fieldName := m.formatter.reMap[s]

		if fieldNameMap, ok := reMap[fieldName]; ok {
			fieldName = fieldNameMap
		}
		fieldValue := vfm.FieldByName(fieldName)
		if !fieldValue.IsValid() {
			if unkField, ok := unkMap[s]; ok {
				fieldValue = reflect.ValueOf(unkField)
			} else {
				continue
			}
		}

		if fieldType, ok := TextFields[s]; ok {
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
		} else if renderer, ok := RenderExtras[s]; ok {
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
					fieldValueI := fieldValue.Index(i)
					if fieldValueI.Elem().Type().Kind() == reflect.String {
						v += fmt.Sprintf("%s%v%s", quotes, fieldValueI.Interface(), quotes)
					} else {
						v += fmt.Sprintf("%v", fieldValueI.Interface())
					}

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
	fstr = fstr[0:i]

	return strings.Join(fstr, sep)
}
