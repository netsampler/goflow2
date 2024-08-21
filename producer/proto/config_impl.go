package protoproducer

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

var (
	isSliceMap = map[string]bool{
		"BgpCommunities":             true,
		"AsPath":                     true,
		"MplsIp":                     true,
		"MplsLabel":                  true,
		"MplsTtl":                    true,
		"LayerStack":                 true,
		"LayerSize":                  true,
		"Ipv6RoutingHeaderAddresses": true,
	}
)

type NetFlowMapField struct {
	PenProvided bool   `yaml:"penprovided"`
	Type        uint16 `yaml:"field"`
	Pen         uint32 `yaml:"pen"`

	Destination string     `yaml:"destination"`
	Endian      EndianType `yaml:"endianness"`
	//DestinationLength uint8  `json:"dlen"` // could be used if populating a slice of uint16 that aren't in protobuf
}

type IPFIXProducerConfig struct {
	Mapping []NetFlowMapField `yaml:"mapping"`
}

type NetFlowV9ProducerConfig struct {
	Mapping []NetFlowMapField `yaml:"mapping"`
}

type SFlowMapField struct {
	Layer        string `yaml:"layer"`
	Encapsulated bool   `yaml:"encap"`  // only parse if encapsulated
	Offset       int    `yaml:"offset"` // offset in bits
	Length       int    `yaml:"length"` // length in bits

	Destination string     `yaml:"destination"`
	Endian      EndianType `yaml:"endianness"`
	//DestinationLength uint8  `json:"dlen"`
}

type SFlowProtocolParse struct {
	Proto  string     `yaml:"proto"`
	Dir    RegPortDir `yaml:"dir"`
	Port   uint16     `yaml:"port"`
	Parser string     `yaml:"parser"`
}

type SFlowProducerConfig struct {
	Mapping []SFlowMapField      `yaml:"mapping"`
	Ports   []SFlowProtocolParse `yaml:"ports"`
}

type ProtobufFormatterConfig struct {
	Name  string `yaml:"name"`
	Index int32  `yaml:"index"`
	Type  string `yaml:"type"`
	Array bool   `yaml:"array"`
}

type FormatterConfig struct {
	Fields   []string                  `yaml:"fields"`
	Key      []string                  `yaml:"key"`
	Render   map[string]RendererID     `yaml:"render"`
	Rename   map[string]string         `yaml:"rename"`
	Protobuf []ProtobufFormatterConfig `yaml:"protobuf"`
}

type ProducerConfig struct {
	Formatter FormatterConfig `yaml:"formatter"`

	IPFIX     IPFIXProducerConfig     `yaml:"ipfix"`
	NetFlowV9 NetFlowV9ProducerConfig `yaml:"netflowv9"`
	SFlow     SFlowProducerConfig     `yaml:"sflow"` // also used for IPFIX data frames

	// should do a rename map list for when printing
}

func (c *ProducerConfig) Compile() (ProtoProducerConfig, error) {
	return mapConfig(c)
}

// Optimized version of a configuration to be used by a protobuf producer
type producerConfigMapped struct {
	Formatter *FormatterConfigMapper

	IPFIX     *NetFlowMapper
	NetFlowV9 *NetFlowMapper
	SFlow     *SFlowMapper
}

func (c *producerConfigMapped) GetFormatter() FormatterMapper {
	return c.Formatter
}

func (c *producerConfigMapped) GetIPFIXMapper() TemplateMapper {
	return c.IPFIX
}

func (c *producerConfigMapped) GetNetFlowMapper() TemplateMapper {
	return c.NetFlowV9
}

func (c *producerConfigMapped) GetPacketMapper() PacketMapper {
	return c.SFlow
}

type DataMap struct {
	MapConfigBase
}

type FormatterConfigMapper struct {
	fields  []string
	key     []string
	reMap   map[string]string // map from a potential json name into the protobuf structure
	rename  map[string]string // manually renaming fields
	render  map[string]RenderFunc
	pbMap   map[string]ProtobufFormatterConfig
	numToPb map[int32]ProtobufFormatterConfig
	isSlice map[string]bool
}

func (f *FormatterConfigMapper) Keys() []string {
	return f.key
}

func (f *FormatterConfigMapper) Fields() []string {
	return f.fields
}

func (f *FormatterConfigMapper) Rename(name string) (string, bool) {
	r, ok := f.rename[name]
	return r, ok
}

func (f *FormatterConfigMapper) Remap(name string) (string, bool) {
	r, ok := f.reMap[name]
	return r, ok
}

func (f *FormatterConfigMapper) Render(name string) (RenderFunc, bool) {
	r, ok := f.render[name]
	return r, ok
}

func (f *FormatterConfigMapper) NumToProtobuf(num int32) (ProtobufFormatterConfig, bool) {
	r, ok := f.numToPb[num]
	return r, ok
}

func (f *FormatterConfigMapper) IsArray(name string) bool {
	return f.isSlice[name]
}

type NetFlowMapper struct {
	data map[string]*DataMap // maps field to destination
}

func (m *NetFlowMapper) Map(field netflow.DataField) (MappableField, bool) {
	if m == nil {
		return &DataMap{}, false
	}
	mapped, found := m.data[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)]
	return mapped, found
}

type SFlowMapper struct {
	data              map[string][]*DataMapLayer // map layer to list of offsets
	parserEnvironment ParserEnvironment
}

type sflowMapperIterator struct {
	data []*DataMapLayer
	n    int
}

func (i *sflowMapperIterator) Next() MappableByteField {
	if len(i.data) <= i.n {
		return nil
	}
	d := i.data[i.n]
	i.n += 1
	return d
}

func (m *SFlowMapper) Map(layer string) MapLayerIterator {
	if m == nil {
		return nil
	}
	return &sflowMapperIterator{data: m.data[strings.ToLower(layer)], n: 0}
}

func (m *SFlowMapper) ParsePacket(flowMessage ProtoProducerMessageIf, data []byte) (err error) {
	if m == nil {
		return ParsePacket(flowMessage, data, m, DefaultEnvironment)
	}
	return ParsePacket(flowMessage, data, m, m.parserEnvironment)
}

// Structure to help the MapCustom functions
// populate the protobuf data
type MapConfigBase struct {
	// Used if the field inside the protobuf exists
	// also serves as the field when rendering with text
	Destination string
	Endianness  EndianType

	// The following fields are used for mapping
	// when the destination field does not exist
	// inside the protobuf
	ProtoIndex int32
	ProtoType  ProtoType
	ProtoArray bool
}

func (c *MapConfigBase) GetEndianness() EndianType {
	return c.Endianness
}

func (c *MapConfigBase) GetDestination() string {
	return c.Destination
}

func (c *MapConfigBase) GetProtoIndex() int32 {
	return c.ProtoIndex
}

func (c *MapConfigBase) GetProtoType() ProtoType {
	return c.ProtoType
}

func (c *MapConfigBase) IsArray() bool {
	return c.ProtoArray
}

// Extended structure for packet mapping
type DataMapLayer struct {
	MapConfigBase
	Offset       int
	Length       int
	Encapsulated bool
}

func (c *DataMapLayer) GetOffset() int {
	return c.Offset
}

func (c *DataMapLayer) GetLength() int {
	return c.Length
}

func (c *DataMapLayer) IsEncapsulated() bool {
	return c.Encapsulated
}

func mapFieldsSFlow(fields []SFlowMapField) *SFlowMapper {
	ret := make(map[string][]*DataMapLayer)
	for _, field := range fields {
		retLayerEntry := &DataMapLayer{
			Offset:       field.Offset,
			Length:       field.Length,
			Encapsulated: field.Encapsulated,
		}
		retLayerEntry.Destination = field.Destination
		retLayerEntry.Endianness = field.Endian
		retLayer := ret[field.Layer]
		retLayer = append(retLayer, retLayerEntry)
		ret[field.Layer] = retLayer
	}
	return &SFlowMapper{data: ret}
}

func mapPortsSFlow(ports []SFlowProtocolParse) (ParserEnvironment, error) {
	e := NewBaseParserEnvironment()
	for _, port := range ports {
		parser, ok := e.GetParser(port.Parser)
		if !ok {
			return e, fmt.Errorf("parser %s not found", port.Parser)
		}
		if err := e.RegisterPort(port.Proto, port.Dir, port.Port, parser); err != nil {
			return e, err
		}
	}
	return e, nil
}

func mapFieldsNetFlow(fields []NetFlowMapField) *NetFlowMapper {
	ret := make(map[string]*DataMap)
	for _, field := range fields {
		dm := &DataMap{}
		dm.Destination = field.Destination
		dm.Endianness = field.Endian
		ret[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)] = dm
	}
	return &NetFlowMapper{data: ret}
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
	if m == nil {
		return nil
	}
	for k, vlist := range m.data {
		for i, v := range vlist {
			if err := c.finalizemapDest(&(v.MapConfigBase)); err != nil {
				return err
			}
			m.data[k][i] = v
		}
	}
	return nil
}

func (c *producerConfigMapped) finalizeNetFlowMapper(m *NetFlowMapper) error {
	if m == nil {
		return nil
	}
	for k, v := range m.data {
		if err := c.finalizemapDest(&(v.MapConfigBase)); err != nil {
			return err
		}
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
	msgT := reflect.TypeOf(&msg.FlowMessage).Elem() // required indirect otherwise go vet indicates TypeOf copies lock
	reMap := make(map[string]string)
	numToPb := make(map[int32]ProtobufFormatterConfig)
	var fields []string

	for i := 0; i < msgT.NumField(); i++ {
		field := msgT.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldName := field.Name
		if selectorTag != "" {
			fieldName = ExtractTag(selectorTag, fieldName, field.Tag)
			reMap[fieldName] = field.Name
			fields = append(fields, fieldName)
		}
		//customSelectorTmp[i] = fieldName

	}

	formatterMapped.reMap = reMap
	pbMap := make(map[string]ProtobufFormatterConfig)
	formatterMapped.render = make(map[string]RenderFunc)
	formatterMapped.rename = make(map[string]string)
	formatterMapped.isSlice = isSliceMap
	for k, v := range defaultRenderers {
		formatterMapped.render[k] = v
	}

	if cfg != nil {
		cfgFormatter := cfg.Formatter

		// manual protobuf fields to add
		for _, pbField := range cfgFormatter.Protobuf {
			reMap[pbField.Name] = ""      // special dynamic protobuf
			pbMap[pbField.Name] = pbField // todo: check if type is valid

			numToPb[pbField.Index] = pbField
			formatterMapped.isSlice[pbField.Name] = pbField.Array
		}
		// populate manual renames
		for k, v := range cfgFormatter.Rename {
			formatterMapped.rename[k] = v
		}

		// populate key
		for _, v := range cfgFormatter.Key {
			if _, ok := reMap[v]; !ok {
				return formatterMapped, fmt.Errorf("key field %s does not exist", v)
			}
			formatterMapped.key = append(formatterMapped.key, v)
		}

		// process renderers
		for k, v := range cfgFormatter.Render {
			if kk, ok := reMap[k]; ok && kk != "" {
				k = kk
			}
			if renderer, ok := renderers[v]; ok {
				formatterMapped.render[k] = renderer
			} else {
				return formatterMapped, fmt.Errorf("field %s is not a renderer", v) // todo: make proper error
			}
		}

		// if the config does not contain any fields initially, we set with the protobuf ones
		if len(cfgFormatter.Fields) == 0 {
			formatterMapped.fields = fields
		} else {
			for _, field := range cfgFormatter.Fields {
				if _, ok := reMap[field]; !ok {

					// check if it's a virtual field
					if _, ok := formatterMapped.render[field]; !ok {
						return formatterMapped, fmt.Errorf("field %s in config not found in protobuf", field) // todo: make proper error
					}

				}
			}
			formatterMapped.fields = cfgFormatter.Fields
		}

		formatterMapped.pbMap = pbMap
		formatterMapped.numToPb = numToPb

	} else {
		formatterMapped.fields = fields
	}

	return formatterMapped, nil
}

func mapConfig(cfg *ProducerConfig) (*producerConfigMapped, error) {
	newCfg := &producerConfigMapped{}
	if cfg != nil {
		newCfg.IPFIX = mapFieldsNetFlow(cfg.IPFIX.Mapping)
		newCfg.NetFlowV9 = mapFieldsNetFlow(cfg.NetFlowV9.Mapping)
		newCfg.SFlow = mapFieldsSFlow(cfg.SFlow.Mapping)
		var err error
		newCfg.SFlow.parserEnvironment, err = mapPortsSFlow(cfg.SFlow.Ports)
		if err != nil {
			return nil, err
		}
	}
	var err error
	if newCfg.Formatter, err = mapFormat(cfg); err != nil {
		return newCfg, err
	}
	return newCfg, newCfg.finalize()
}
