package protoproducer

import (
	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

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

// MappableField is the interface that allows a flow's field to be mapped to a specific protobuf field.
// Provided by Template Mapper's function.
type MappableField interface {
	GetEndianness() EndianType
	GetDestination() string
	GetProtoIndex() int32
	GetProtoType() ProtoType
	IsArray() bool
}

// MappableByteField is the interface, similar to MappableField, but for direct packet parsing.
// Provided by PacketMapper.
type MappableByteField interface {
	MappableField
	GetOffset() int
	GetLength() int
	IsEncapsulated() bool
}

// TemplateMapper is the interface to returns the mapping information for a specific type of template field
type TemplateMapper interface {
	Map(field netflow.DataField) (MappableField, bool)
}

// MapLayerIterator is the interface to obtain subsequent mapping information
type MapLayerIterator interface {
	Next() MappableByteField // returns the next MappableByteField. Function is called by the packet parser until returns nil.
}

// PacketLayerMapper is the interface to obtain the mapping information for a layer of a packet
type PacketLayerMapper interface {
	Map(layer string) MapLayerIterator // returns an iterator to avoid handling arrays
}

// PacketMapper is the interface to parse a packet into a flow message
type PacketMapper interface {
	ParsePacket(flowMessage ProtoProducerMessageIf, data []byte) (err error)
}

// FormatterMapper returns the configuration statements for the textual formatting of the protobuf messages
type FormatterMapper interface {
	Keys() []string
	Fields() []string
	Rename(name string) (string, bool)
	Remap(name string) (string, bool)
	Render(name string) (RenderFunc, bool)
	NumToProtobuf(num int32) (ProtobufFormatterConfig, bool)
	IsArray(name string) bool
}

// ProtoProducerConfig is the top level configuration for a general flow to protobuf producer
type ProtoProducerConfig interface {
	GetFormatter() FormatterMapper
	GetIPFIXMapper() TemplateMapper
	GetNetFlowMapper() TemplateMapper
	GetPacketMapper() PacketMapper
}
