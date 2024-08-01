package protoproducer

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

type producerConfigMapped struct {
	Formatter *FormatterConfigMapper

	IPFIX     *NetFlowMapper
	NetFlowV9 *NetFlowMapper
	SFlow     *SFlowMapper
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

type NetFlowMapper struct {
	data map[string]DataMap // maps field to destination
}

type SFlowMapper struct {
	data map[string][]DataMapLayer // map layer to list of offsets
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

type MappingConfigIf interface {
	GetLayer(layer string)
}
