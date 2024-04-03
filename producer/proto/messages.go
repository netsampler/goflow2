package protoproducer

import (
	"bytes"
	"fmt"
	"hash"
	"hash/fnv"
	"reflect"
	"strings"
	"sync"

	"google.golang.org/protobuf/encoding/protodelim"
	"google.golang.org/protobuf/encoding/protowire"

	flowmessage "github.com/netsampler/goflow2/v2/pb"
)

type ProtoProducerMessage struct {
	flowmessage.FlowMessage

	formatter *FormatterConfigMapper
}

var protoMessagePool = sync.Pool{
	New: func() any {
		return &ProtoProducerMessage{}
	},
}

func (m *ProtoProducerMessage) MarshalBinary() ([]byte, error) {
	buf := bytes.NewBuffer([]byte{})
	_, err := protodelim.MarshalTo(buf, m)
	return buf.Bytes(), err
}

func (m *ProtoProducerMessage) MarshalText() ([]byte, error) {
	return []byte(m.FormatMessageReflectText("")), nil
}

func (m *ProtoProducerMessage) baseKey(h hash.Hash) {
	vfm := reflect.ValueOf(m)
	vfm = reflect.Indirect(vfm)

	unkMap := m.mapUnknown() // todo: should be able to reuse if set in structure

	for _, s := range m.formatter.key {
		fieldName := s

		// get original name from structure
		if fieldNameMap, ok := m.formatter.reMap[fieldName]; ok && fieldNameMap != "" {
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
	if m.formatter == nil || len(m.formatter.key) == 0 {
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
		if pbField, ok := m.formatter.numToPb[int32(num)]; ok {

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
	fstr := make([]string, len(m.formatter.fields)) // todo: reuse with pool

	unkMap := m.mapUnknown()

	// iterate through the fields requested by the user
	for _, s := range m.formatter.fields {
		fieldName := s

		fieldFinalName := s
		if fieldRename, ok := m.formatter.rename[s]; ok && fieldRename != "" {
			fieldFinalName = fieldRename
		}

		// get original name from structure
		if fieldNameMap, ok := m.formatter.reMap[fieldName]; ok && fieldNameMap != "" {
			fieldName = fieldNameMap
		}

		// get renderer
		renderer, okRenderer := m.formatter.render[fieldName]
		if !okRenderer {
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

		isSlice := m.formatter.isSlice[fieldName]

		// render each item of the array independently
		// note: isSlice is necessary to consider certain byte arrays in their entirety
		// eg: IP addresses
		if isSlice {
			c := fieldValue.Len()
			v := "["
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
