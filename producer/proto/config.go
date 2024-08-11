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

type MapConfigBaseIf interface {
	GetEndianness() EndianType
	GetDestination() string
	GetProtoIndex() int32
	GetProtoType() ProtoType
	IsArray() bool
}

type MapConfigLayerIf interface {
	MapConfigBaseIf
	GetOffset() int
	GetLength() int
	IsEncapsulated() bool
}

// Returns the mapping information for a specific type of template field
type TemplateMapper interface {
	Map(field netflow.DataField) (MapConfigBaseIf, bool)
}

// Returns the mapping information for a layer of a packet
type PacketMapper interface {
	Map(layer string) MapLayerIterator
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
	GetFormatter() FormatterMapper
	GetIPFIXMapper() TemplateMapper
	GetNetFlowMapper() TemplateMapper
	GetPacketMapper() PacketMapper
}
