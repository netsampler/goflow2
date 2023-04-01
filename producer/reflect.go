package producer

import (
	"fmt"
	"reflect"

	"github.com/netsampler/goflow2/decoders/netflow"
)

type EndianType string

var (
	BigEndian    EndianType = "big"
	LittleEndian EndianType = "little"
)

func GetBytes(d []byte, offset int, length int) []byte {
	if length == 0 {
		return nil
	}
	leftBytes := offset / 8
	rightBytes := (offset + length) / 8
	if (offset+length)%8 != 0 {
		rightBytes += 1
	}
	if leftBytes >= len(d) {
		return nil
	}
	if rightBytes > len(d) {
		rightBytes = len(d)
	}
	chunk := make([]byte, rightBytes-leftBytes)

	offsetMod8 := (offset % 8)
	shiftAnd := byte(0xff >> (8 - offsetMod8))

	var shifted byte
	for i := range chunk {
		j := len(chunk) - 1 - i
		cur := d[j+leftBytes]
		chunk[j] = (cur << offsetMod8) | shifted
		shifted = shiftAnd & cur
	}
	last := len(chunk) - 1
	shiftAndLast := byte(0xff << ((8 - ((offset + length) % 8)) % 8))
	chunk[last] = chunk[last] & shiftAndLast
	return chunk
}

func IsUInt(k reflect.Kind) bool {
	return k == reflect.Uint8 || k == reflect.Uint16 || k == reflect.Uint32 || k == reflect.Uint64
}

func IsInt(k reflect.Kind) bool {
	return k == reflect.Int8 || k == reflect.Int16 || k == reflect.Int32 || k == reflect.Int64
}

func MapCustomNetFlow(flowMessage *ProtoProducerMessage, df netflow.DataField, mapper *NetFlowMapper) {
	if mapper == nil {
		return
	}
	mapped, ok := mapper.Map(df)
	if ok {
		v := df.Value.([]byte)
		MapCustom(flowMessage, v, mapped.Destination, mapped.Endian)
	}
}

func MapCustom(flowMessage *ProtoProducerMessage, v []byte, destination string, endianness EndianType) {
	vfm := reflect.ValueOf(flowMessage)
	vfm = reflect.Indirect(vfm)

	fieldValue := vfm.FieldByName(destination)

	if fieldValue.IsValid() {
		typeDest := fieldValue.Type()
		fieldValueAddr := fieldValue.Addr()

		if typeDest.Kind() == reflect.Slice {

			if typeDest.Elem().Kind() == reflect.Uint8 {
				fieldValue.SetBytes(v)
			} else {
				item := reflect.New(typeDest.Elem())

				if IsUInt(typeDest.Elem().Kind()) {
					if endianness == LittleEndian {
						DecodeUNumberLE(v, item.Interface())
					} else {
						DecodeUNumber(v, item.Interface())
					}
				} else if IsUInt(typeDest.Elem().Kind()) {
					if endianness == LittleEndian {
						DecodeUNumberLE(v, item.Interface())
					} else {
						DecodeUNumber(v, item.Interface())
					}
				}

				itemi := reflect.Indirect(item)
				tmpFieldValue := reflect.Append(fieldValue, itemi)
				fieldValue.Set(tmpFieldValue)
			}

		} else if fieldValueAddr.IsValid() && IsUInt(typeDest.Kind()) {
			if endianness == LittleEndian {
				DecodeUNumberLE(v, fieldValueAddr.Interface())
			} else {
				DecodeUNumber(v, fieldValueAddr.Interface())
			}
		} else if fieldValueAddr.IsValid() && IsInt(typeDest.Kind()) {
			if endianness == LittleEndian {
				DecodeUNumberLE(v, fieldValueAddr.Interface())
			} else {
				DecodeUNumber(v, fieldValueAddr.Interface())
			}
		}
	}
}

type NetFlowMapField struct {
	PenProvided bool   `json:"penprovided" yaml:"penprovided"`
	Type        uint16 `json:"field" yaml:"field"`
	Pen         uint32 `json:"pen" yaml:"pen"`

	Destination string     `json:"destination" yaml:"destination"`
	Endian      EndianType `json:"endianness" yaml:"endianness"`
	//DestinationLength uint8  `json:"dlen"` // could be used if populating a slice of uint16 that aren't in protobuf
}

type IPFIXProducerConfig struct {
	Mapping []NetFlowMapField `json:"mapping"`
	//PacketMapping []SFlowMapField   `json:"packet-mapping"` // for embedded frames: use sFlow configuration
}

type NetFlowV9ProducerConfig struct {
	Mapping []NetFlowMapField `json:"mapping"`
}

type SFlowMapField struct {
	Layer  int `json:"layer"`
	Offset int `json:"offset"` // offset in bits
	Length int `json:"length"` // length in bits

	Destination string     `json:"destination" yaml:"destination"`
	Endian      EndianType `json:"endianness" yaml:"endianness"`
	//DestinationLength uint8  `json:"dlen"`
}

type SFlowProducerConfig struct {
	Mapping []SFlowMapField `json:"mapping"`
}

type ProtobufFormatterConfig struct {
	Name  string
	Index int
	Type  string
}

type FormatterConfig struct {
	Fields   []string
	Key      []string
	Rename   map[string]string
	Protobuf []ProtobufFormatterConfig
}

type ProducerConfig struct {
	Formatter FormatterConfig `json:"formatter"`

	IPFIX     IPFIXProducerConfig     `json:"ipfix"`
	NetFlowV9 NetFlowV9ProducerConfig `json:"netflowv9"`
	SFlow     SFlowProducerConfig     `json:"sflow"` // also used for IPFIX data frames

	// should do a rename map list for when printing
}

type DataMap struct {
	Destination string
	Endian      EndianType
}

type FormatterConfigMapper struct {
	fields []string
	reMap  map[string]string
	proto  map[string]int
}

type NetFlowMapper struct {
	data map[string]DataMap // maps field to destination
}

func (m *NetFlowMapper) Map(field netflow.DataField) (DataMap, bool) {
	mapped, found := m.data[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)]
	return mapped, found
}

func MapFieldsNetFlow(fields []NetFlowMapField) *NetFlowMapper {
	ret := make(map[string]DataMap)
	for _, field := range fields {
		ret[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)] = DataMap{Destination: field.Destination, Endian: field.Endian}
	}
	return &NetFlowMapper{ret}
}

type DataMapLayer struct {
	Offset      int
	Length      int
	Destination string
	Endian      EndianType
}

type SFlowMapper struct {
	data map[int][]DataMapLayer // map layer to list of offsets
}

func GetSFlowConfigLayer(m *SFlowMapper, layer int) []DataMapLayer {
	if m == nil {
		return nil
	}
	return m.data[layer]
}

func MapFieldsSFlow(fields []SFlowMapField) *SFlowMapper {
	ret := make(map[int][]DataMapLayer)
	for _, field := range fields {
		retLayerEntry := DataMapLayer{
			Offset:      field.Offset,
			Length:      field.Length,
			Destination: field.Destination,
			Endian:      field.Endian,
		}
		retLayer := ret[field.Layer]
		retLayer = append(retLayer, retLayerEntry)
		ret[field.Layer] = retLayer
	}
	return &SFlowMapper{ret}
}

type producerConfigMapped struct {
	Formatter *FormatterConfigMapper

	IPFIX     *NetFlowMapper `json:"ipfix"`
	NetFlowV9 *NetFlowMapper `json:"netflowv9"`
	SFlow     *SFlowMapper   `json:"sflow"`
}

func mapFormat(cfg *ProducerConfig) (*FormatterConfigMapper, error) {
	formatterMapped := &FormatterConfigMapper{}

	selectorTag := "json"
	var msg ProtoProducerMessage
	msgT := reflect.TypeOf(msg.FlowMessage)
	reMap := make(map[string]string)
	var fields []string

	for i := 0; i < msgT.NumField(); i++ {
		field := msgT.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldName := field.Name
		if selectorTag != "" {
			fieldName = ExtractTag(selectorTag, field.Name, field.Tag)
			reMap[fieldName] = field.Name
			fields = append(fields, fieldName)
		}
		//customSelectorTmp[i] = fieldName

	}
	formatterMapped.reMap = reMap

	fmt.Println(reMap)
	fmt.Println(fields)

	pbMap := make(map[string]int)
	pbTypes := make(map[string]string)

	if cfg != nil {
		cfgFormatter := cfg.Formatter
		for _, pbField := range cfgFormatter.Protobuf {
			reMap[pbField.Name] = "" // special dynamic protobuf
			pbMap[pbField.Name] = pbField.Index
			pbTypes[pbField.Name] = pbField.Type // todo: check if type is valid
		}
		if len(cfgFormatter.Fields) == 0 {
			formatterMapped.fields = fields
		} else {
			for _, field := range cfgFormatter.Fields {
				if _, ok := reMap[field]; !ok {
					return formatterMapped, fmt.Errorf("field %s in config not found in protobuf", field) // todo: make proper error
				}
			}
			formatterMapped.fields = cfgFormatter.Fields
		}

	}
	fmt.Println(pbMap)

	return formatterMapped, nil
}

func mapConfig(cfg *ProducerConfig) (*producerConfigMapped, error) {
	newCfg := &producerConfigMapped{}
	if cfg != nil {
		newCfg.IPFIX = MapFieldsNetFlow(cfg.IPFIX.Mapping)
		newCfg.NetFlowV9 = MapFieldsNetFlow(cfg.NetFlowV9.Mapping)
		newCfg.SFlow = MapFieldsSFlow(cfg.SFlow.Mapping)
	}
	var err error
	newCfg.Formatter, err = mapFormat(cfg)
	return newCfg, err
}
