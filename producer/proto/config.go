package protoproducer

import (
	"fmt"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
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
	Layer string `yaml:"layer"`
	//Encapsulated bool `yaml:"encap"` // only parse if encapsulated
	Offset int `yaml:"offset"` // offset in bits
	Length int `yaml:"length"` // length in bits

	Destination string     `yaml:"destination"`
	Endian      EndianType `yaml:"endianness"`
	//DestinationLength uint8  `json:"dlen"`
}

type SFlowProducerConfig struct {
	Mapping []SFlowMapField `yaml:"mapping"`
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

func (c *producerConfigMapped) GetFormatter() *FormatterConfigMapper {
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
	data map[string]DataMap // maps field to destination
}

func (m *NetFlowMapper) Map(field netflow.DataField) (DataMap, bool) {
	if m == nil {
		return DataMap{}, false
	}
	mapped, found := m.data[fmt.Sprintf("%v-%d-%d", field.PenProvided, field.Pen, field.Type)]
	return mapped, found
}

type SFlowMapper struct {
	data map[string][]DataMapLayer // map layer to list of offsets
}

func (m *SFlowMapper) Map(layer string) []DataMapLayer {
	if m == nil {
		return nil
	}
	return m.data[layer]
}

type EndianType string
type ProtoType string

var (
	BigEndian    EndianType = "big"
	LittleEndian EndianType = "little"

	ProtoString ProtoType = "string"
	ProtoVarint ProtoType = "varint"

	ProtoTypeMap = map[string]ProtoType{
		string(ProtoString): ProtoString,
		string(ProtoVarint): ProtoVarint,
		"bytes":             ProtoString,
	}
)

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
	Offset int
	Length int
}

// Refactoring using interfaces

type MapConfigBaseIf interface {
	GetEndianness() EndianType
	GetDestination() string
	GetProtoIndex() int32
	GetProtoType() ProtoType
	IsArray() bool
}

type MapConfigLayerIf interface {
	// combine with configbase?
	GetOffset() int
	GetLength() int
}

// Returns the mapping information for a specific type of template field
type TemplateMapper interface {
	Map(field netflow.DataField) (DataMap, bool)
}

// Returns the mapping information for a layer of a packet
type PacketMapper interface {
	Map(layer string) []DataMapLayer
}

type FormatterMapper interface {
	Keys() []string
	Fields() []string
	Rename(name string) (string, bool)
	Remap(name string) (string, bool)
	Render(name string) (RenderFunc, bool)
	NumToProtobuf(num int32) (ProtobufFormatterConfig, bool)
	IsArray(name string) bool
}

// Top level configuration for a general flow to protobuf producer
type ProtoProducerConfig interface {
	GetFormatter() *FormatterConfigMapper
	GetIPFIXMapper() TemplateMapper
	GetNetFlowMapper() TemplateMapper
	GetPacketMapper() PacketMapper
}
