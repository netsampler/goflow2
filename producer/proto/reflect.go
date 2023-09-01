package protoproducer

import (
	"fmt"
	"reflect"

	"google.golang.org/protobuf/encoding/protowire"

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

func GetBytes(d []byte, offset int, length int) []byte {
	if length == 0 || offset < 0 {
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

func MapCustomNetFlow(flowMessage *ProtoProducerMessage, df netflow.DataField, mapper *NetFlowMapper) error {
	if mapper == nil {
		return nil
	}
	mapped, ok := mapper.Map(df)
	if ok {
		v := df.Value.([]byte)
		if err := MapCustom(flowMessage, v, mapped.MapConfigBase); err != nil {
			return err
		}
	}
	return nil
}

func MapCustom(flowMessage *ProtoProducerMessage, v []byte, cfg MapConfigBase) error {
	vfm := reflect.ValueOf(flowMessage)
	vfm = reflect.Indirect(vfm)

	fieldValue := vfm.FieldByName(cfg.Destination)

	if fieldValue.IsValid() {
		typeDest := fieldValue.Type()
		fieldValueAddr := fieldValue.Addr()

		if typeDest.Kind() == reflect.Slice {

			if typeDest.Elem().Kind() == reflect.Uint8 {
				fieldValue.SetBytes(v)
			} else {
				item := reflect.New(typeDest.Elem())

				if IsUInt(typeDest.Elem().Kind()) {
					if cfg.Endianness == LittleEndian {
						if err := DecodeUNumberLE(v, item.Interface()); err != nil {
							return err
						}
					} else {
						if err := DecodeUNumber(v, item.Interface()); err != nil {
							return err
						}
					}
				} else if IsInt(typeDest.Elem().Kind()) {
					if cfg.Endianness == LittleEndian {
						if err := DecodeNumberLE(v, item.Interface()); err != nil {
							return err
						}
					} else {
						if err := DecodeNumber(v, item.Interface()); err != nil {
							return err
						}
					}
				}

				itemi := reflect.Indirect(item)
				tmpFieldValue := reflect.Append(fieldValue, itemi)
				fieldValue.Set(tmpFieldValue)
			}

		} else if fieldValueAddr.IsValid() {
			if IsUInt(typeDest.Kind()) {
				if cfg.Endianness == LittleEndian {
					if err := DecodeUNumberLE(v, fieldValueAddr.Interface()); err != nil {
						return err
					}
				} else {
					if err := DecodeUNumber(v, fieldValueAddr.Interface()); err != nil {
						return err
					}
				}
			} else if IsInt(typeDest.Kind()) {
				if cfg.Endianness == LittleEndian {
					if err := DecodeNumberLE(v, fieldValueAddr.Interface()); err != nil {
						return err
					}
				} else {
					if err := DecodeNumber(v, fieldValueAddr.Interface()); err != nil {
						return err
					}
				}
			}

		}
	} else if cfg.ProtoIndex > 0 {

		fmr := flowMessage.ProtoReflect()
		unk := fmr.GetUnknown()

		if !cfg.ProtoArray {
			var offset int
			for offset < len(unk) {
				num, _, length := protowire.ConsumeField(unk[offset:])
				offset += length
				if int32(num) == cfg.ProtoIndex {
					// only one allowed
					break
				}
			}
		}

		var dstVar uint64
		if cfg.ProtoType == ProtoVarint {
			if cfg.Endianness == LittleEndian {
				if err := DecodeUNumberLE(v, &dstVar); err != nil {
					return err
				}
			} else {
				if err := DecodeUNumber(v, &dstVar); err != nil {
					return err
				}
			}
			// support signed int?
			unk = protowire.AppendTag(unk, protowire.Number(cfg.ProtoIndex), protowire.VarintType)
			unk = protowire.AppendVarint(unk, dstVar)
		} else if cfg.ProtoType == ProtoString {
			unk = protowire.AppendTag(unk, protowire.Number(cfg.ProtoIndex), protowire.BytesType)
			unk = protowire.AppendString(unk, string(v))
		} else {
			return fmt.Errorf("could not insert into protobuf unknown")
		}
		fmr.SetUnknown(unk)
	}
	return nil
}
