package producer

import (
	"fmt"
	"reflect"

	"github.com/netsampler/goflow2/decoders/netflow"
)

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
	Index int32
	Type  string
	Array bool
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
	MapConfigBase
}

type FormatterConfigMapper struct {
	fields []string
	reMap  map[string]string
	pbMap  map[string]ProtobufFormatterConfig
}

type NetFlowMapper struct {
	data map[string]DataMap // maps field to destination
}

func (m *NetFlowMapper) Map(field netflow.DataField) (DataMap, bool) {
	mapped, found := m.data[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)]
	return mapped, found
}

type DataMapLayer struct {
	MapConfigBase
	Offset int
	Length int
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
			Offset: field.Offset,
			Length: field.Length,
		}
		retLayerEntry.Destination = field.Destination
		retLayerEntry.Endianness = field.Endian
		retLayer := ret[field.Layer]
		retLayer = append(retLayer, retLayerEntry)
		ret[field.Layer] = retLayer
	}
	return &SFlowMapper{ret}
}

func MapFieldsNetFlow(fields []NetFlowMapField) *NetFlowMapper {
	ret := make(map[string]DataMap)
	for _, field := range fields {
		dm := DataMap{}
		dm.Destination = field.Destination
		dm.Endianness = field.Endian
		ret[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)] = dm
	}
	return &NetFlowMapper{ret}
}

type producerConfigMapped struct {
	Formatter *FormatterConfigMapper

	IPFIX     *NetFlowMapper
	NetFlowV9 *NetFlowMapper
	SFlow     *SFlowMapper
}

func (c *producerConfigMapped) finalizemapDest(v *MapConfigBase) error {
	if vv, ok := c.Formatter.pbMap[v.Destination]; ok {
		v.ProtoIndex = vv.Index

		if pt, ok := ProtoTypeMap[vv.Type]; ok {
			v.ProtoType = pt
		} else {
			return fmt.Errorf("could not map %s to a ProtoType", vv.Type)
		}

		v.ProtoArray = vv.Array
	}
	return nil
}
func (c *producerConfigMapped) finalizeSFlowMapper(m *SFlowMapper) error {

	for k, vlist := range m.data {
		for i, v := range vlist {
			c.finalizemapDest(&(v.MapConfigBase))
			m.data[k][i] = v
		}

	}
	return nil
}

func (c *producerConfigMapped) finalizeNetFlowMapper(m *NetFlowMapper) error {

	for k, v := range m.data {
		c.finalizemapDest(&(v.MapConfigBase))
		m.data[k] = v
	}
	return nil
}

func (c *producerConfigMapped) finalize() error {
	if c.Formatter == nil {
		return nil
	}
	if err := c.finalizeNetFlowMapper(c.IPFIX); err != nil {
		return err
	}
	if err := c.finalizeNetFlowMapper(c.NetFlowV9); err != nil {
		return err
	}
	if err := c.finalizeSFlowMapper(c.SFlow); err != nil {
		return err
	}

	return nil
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

	//fmt.Println(reMap)
	//fmt.Println(fields)

	pbMap := make(map[string]ProtobufFormatterConfig)

	if cfg != nil {
		cfgFormatter := cfg.Formatter

		// manual protobuf fields to add
		for _, pbField := range cfgFormatter.Protobuf {
			reMap[pbField.Name] = ""      // special dynamic protobuf
			pbMap[pbField.Name] = pbField // todo: check if type is valid
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

		formatterMapped.pbMap = pbMap

	}
	//fmt.Println(pbMap)
	//fmt.Println(formatterMapped.fields)

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
	if newCfg.Formatter, err = mapFormat(cfg); err != nil {
		return newCfg, err
	}
	return newCfg, newCfg.finalize()
}
