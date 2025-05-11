package protoproducer

import (
	"bytes"
	"fmt"
	"hash"
	"hash/fnv"
	"log"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	flowmessage "github.com/netsampler/goflow2/v2/pb"
)

// ProtoProducerMessageIf interface to a flow message, used by parsers and tests
type ProtoProducerMessageIf interface {
	GetFlowMessage() *ProtoProducerMessage                   // access the underlying structure
	MapCustom(key string, v []byte, cfg MappableField) error // inject custom field
}

type ProtoProducerMessage struct {
	flowmessage.FlowMessage

	formatter FormatterMapper

	skipDelimiter bool // for binary marshalling, skips the varint prefix
}

var protoMessagePool = sync.Pool{
	New: func() any {
		return &ProtoProducerMessage{}
	},
}

func (m *ProtoProducerMessage) GetFlowMessage() *ProtoProducerMessage {
	return m
}

func (m *ProtoProducerMessage) MapCustom(key string, v []byte, cfg MappableField) error {
	return MapCustom(m, v, cfg)
}

func (m *ProtoProducerMessage) AddLayer(name string) (ok bool) {
	value, ok := flowmessage.FlowMessage_LayerStack_value[name]
	m.LayerStack = append(m.LayerStack, flowmessage.FlowMessage_LayerStack(value))
	return ok
}

func (m *ProtoProducerMessage) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer([]byte{})

	if m.skipDelimiter {
		b, err := proto.Marshal(m)
		if err != nil {
			log.Printf("Marshal error: %v", err)
			return nil, err
		}

		// Parse fields to find tag 13
		offset := 0

		for offset < len(b) {
			num, wireType, n := protowire.ConsumeTag(b[offset:])

			if n < 0 {
				log.Printf("Invalid tag at offset %d: %x", offset, b[offset:offset+min(10, len(b)-offset)])

				return b, fmt.Errorf("invalid tag at offset %d", offset)
			}

			offset += n

			if num == 13 {
				log.Printf("Found bytes field (tag 13) with WireType %d at offset %d", wireType, offset-n)

				if wireType != protowire.VarintType {
					log.Printf("Invalid wire type %d for bytes field, expected Varint (0)", wireType)
					log.Printf("Bytes around tag 13: %x", b[max(0, offset-n-5):min(len(b), offset+10)])
				}
			}

			switch wireType {
			case protowire.VarintType:
				v, n := protowire.ConsumeVarint(b[offset:])
				if n < 0 {
					log.Printf("Invalid varint at offset %d for tag %d: %x", offset, num, b[offset:offset+min(10, len(b)-offset)])
					break
				}

				log.Printf("Field %d (Varint): %d", num, v)

				offset += n
			case protowire.BytesType:
				v, n := protowire.ConsumeBytes(b[offset:])
				if n < 0 {
					log.Printf("Invalid bytes at offset %d for tag %d: %x", offset, num, b[offset:offset+min(10, len(b)-offset)])
					break
				}

				log.Printf("Field %d (Bytes): %x", num, v)
				offset += n
			case protowire.Fixed32Type:
				if offset+4 > len(b) {
					log.Printf("Incomplete fixed32 at offset %d for tag %d: %x", offset, num, b[offset:offset+min(10, len(b)-offset)])
					break
				}

				v, n := protowire.ConsumeFixed32(b[offset:])
				log.Printf("Field %d (Fixed32): %d", num, v)

				offset += n
			case protowire.Fixed64Type:
				if offset+8 > len(b) {
					log.Printf("Incomplete fixed64 at offset %d for tag %d: %x", offset, num, b[offset:offset+min(10, len(b)-offset)])
					break
				}

				v, n := protowire.ConsumeFixed64(b[offset:])
				log.Printf("Field %d (Fixed64): %d", num, v)

				offset += n
			default:
				log.Printf("Unknown wire type %d for tag %d at offset %d", wireType, num, offset)
				remaining := len(b) - offset

				log.Printf("Skipping %d bytes for tag %d: %x", remaining, num, b[offset:offset+remaining])
				offset += remaining
			}
		}

		log.Printf("Serialized bytes (hex): %x", b)

		return b, err
	} else {
		_, err := protodelim.MarshalTo(buf, m)

		return buf.Bytes(), err
	}
}

func (m *ProtoProducerMessage) MarshalText() ([]byte, error) {
	return []byte(m.FormatMessageReflectText("")), nil
}

func (m *ProtoProducerMessage) baseKey(h hash.Hash) {
	vfm := reflect.ValueOf(m)
	vfm = reflect.Indirect(vfm)

	unkMap := m.mapUnknown() // todo: should be able to reuse if set in structure

	for _, s := range m.formatter.Keys() {
		fieldName := s

		// get original name from structure
		if fieldNameMap, ok := m.formatter.Remap(fieldName); ok && fieldNameMap != "" {
			fieldName = fieldNameMap
		}

		fieldValue := vfm.FieldByName(fieldName)
		// if does not exist from structure,
		// fetch from unknown (only numbered) fields
		// that were parsed above

		if !fieldValue.IsValid() {
			if unkField, ok := unkMap[s]; ok {
				fieldValue = reflect.ValueOf(unkField)
			} else {
				continue
			}
		}
		h.Write([]byte(fmt.Sprintf("%v", fieldValue.Interface())))
	}
}

func (m *ProtoProducerMessage) Key() []byte {
	if len(m.formatter.Keys()) == 0 {
		return nil
	}
	h := fnv.New32()
	m.baseKey(h)
	return h.Sum(nil)
}

func (m *ProtoProducerMessage) MarshalJSON() ([]byte, error) {
	return []byte(m.FormatMessageReflectJSON("")), nil
}

func (m *ProtoProducerMessage) FormatMessageReflectText(ext string) string {
	return m.FormatMessageReflectCustom(ext, "", " ", "=", false)
}

func (m *ProtoProducerMessage) FormatMessageReflectJSON(ext string) string {
	return fmt.Sprintf("{%s}", m.FormatMessageReflectCustom(ext, "\"", ",", ":", true))
}

func ExtractTag(name, original string, tag reflect.StructTag) string {
	lookup, ok := tag.Lookup(name)
	if !ok {
		return original
	}
	before, _, _ := strings.Cut(lookup, ",")
	return before
}

func (m *ProtoProducerMessage) mapUnknown() map[string]interface{} {
	unkMap := make(map[string]interface{})

	fmr := m.ProtoReflect()
	unk := fmr.GetUnknown()
	var offset int
	for offset < len(unk) {
		num, dataType, length := protowire.ConsumeTag(unk[offset:])
		offset += length
		length = protowire.ConsumeFieldValue(num, dataType, unk[offset:])
		data := unk[offset : offset+length]
		offset += length

		// we check if the index is listed in the config
		if pbField, ok := m.formatter.NumToProtobuf(int32(num)); ok {

			var dest interface{}
			var value interface{}
			if dataType == protowire.VarintType {
				v, _ := protowire.ConsumeVarint(data)
				value = v
			} else if dataType == protowire.BytesType {
				v, _ := protowire.ConsumeString(data)
				//value = hex.EncodeToString([]byte(v)) // removed, this conversion is left to the renderer
				value = []byte(v)
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
	return unkMap
}

func (m *ProtoProducerMessage) FormatMessageReflectCustom(ext, quotes, sep, sign string, null bool) string {
	vfm := reflect.ValueOf(m)
	vfm = reflect.Indirect(vfm)

	var i int
	fields := m.formatter.Fields()
	fstr := make([]string, len(fields)) // todo: reuse with pool

	unkMap := m.mapUnknown()

	// iterate through the fields requested by the user
	for _, s := range fields {
		fieldName := s

		fieldFinalName := s
		if fieldRename, ok := m.formatter.Rename(s); ok && fieldRename != "" {
			fieldFinalName = fieldRename
		}

		// get original name from structure
		if fieldNameMap, ok := m.formatter.Remap(fieldName); ok && fieldNameMap != "" {
			fieldName = fieldNameMap
		}

		// get renderer
		renderer, okRenderer := m.formatter.Render(fieldName)
		if !okRenderer { // todo: change to renderer check
			renderer = NilRenderer
		}

		fieldValue := vfm.FieldByName(fieldName)
		// if does not exist from structure,
		// fetch from unknown (only numbered) fields
		// that were parsed above

		if !fieldValue.IsValid() {
			if unkField, ok := unkMap[s]; ok {
				fieldValue = reflect.ValueOf(unkField)
			} else if !okRenderer { // not a virtual field
				continue
			}
		}

		isSlice := m.formatter.IsArray(fieldName)

		// render each item of the array independently
		// note: isSlice is necessary to consider certain byte arrays in their entirety
		// eg: IP addresses
		if isSlice {
			v := "["

			if fieldValue.IsValid() {

				c := fieldValue.Len()
				for i := 0; i < c; i++ {
					fieldValueI := fieldValue.Index(i)
					var val interface{}
					if fieldValueI.IsValid() {
						val = fieldValueI.Interface()
					}

					rendered := renderer(m, fieldName, val)
					if rendered == nil {
						continue
					}
					renderedType := reflect.TypeOf(rendered)
					if renderedType.Kind() == reflect.String {
						v += fmt.Sprintf("%s%v%s", quotes, rendered, quotes)
					} else {
						v += fmt.Sprintf("%v", rendered)
					}

					if i < c-1 {
						v += ","
					}
				}
			}
			v += "]"
			fstr[i] = fmt.Sprintf("%s%s%s%s%s", quotes, fieldFinalName, quotes, sign, v)
		} else {
			var val interface{}
			if fieldValue.IsValid() {
				val = fieldValue.Interface()
			}

			rendered := renderer(m, fieldName, val)
			if rendered == nil {
				continue
			}
			renderedType := reflect.TypeOf(rendered)
			if renderedType.Kind() == reflect.String {
				fstr[i] = fmt.Sprintf("%s%s%s%s%s%v%s", quotes, fieldFinalName, quotes, sign, quotes, rendered, quotes)
			} else {
				fstr[i] = fmt.Sprintf("%s%s%s%s%v", quotes, fieldFinalName, quotes, sign, rendered)
			}
		}
		i++

	}

	fstr = fstr[0:i]

	return strings.Join(fstr, sep)
}
