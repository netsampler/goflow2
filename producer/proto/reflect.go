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

// Using a data slice, returns a chunk corresponding
func GetBytes2(d []byte, offset, length int, shift bool) []byte {

	/*

		Example with an offset of 4 and length of 6

		initial data:
		0xAA 0x55
		  1010 1010.0101 0101
		       ^--- -^

		with shift
		0x29
		  0010 1001

		without shift (bitwise AND)
		0xa4
		  1010 0100


	*/

	if len(d)*8 < offset {
		return nil
	}
	if length == 0 {
		return nil
	}

	shiftSize := offset % 8      // how much to shift to the left due to offset
	shiftRightSize := length % 8 // for final step

	start := offset / 8
	end := (offset + length) / 8
	if (offset+length)%8 > 0 {
		end += 1
	}

	lengthB := length / 8
	if shiftRightSize > 0 {
		lengthB += 1
	}

	missing := end - len(d) // calculate how many missing bytes
	if missing > 0 {
		end = len(d)
	}

	dUsed := d[start:end]

	if shiftSize == 0 && length%8 == 0 { // simple case
		if missing > 0 {
			dFinal := make([]byte, lengthB)

			copy(dFinal, dUsed)
			return dFinal
		}
		return dUsed
	}

	dFinal := make([]byte, lengthB)

	// first pass, apply offset
	for i := range dFinal {
		if i >= len(dUsed) {
			break
		}
		left := dUsed[i] << shiftSize

		dFinal[i] = left
		if i+1 >= len(dUsed) {
			break
		}
		right := dUsed[i+1] >> (8 - shiftSize)
		dFinal[i] |= right
	}

	// final pass
	if shift {
		dFinal[len(dFinal)-1] >>= (8 - shiftRightSize) % 8
	} else {
		dFinal[len(dFinal)-1] &= (0xFF << ((8 - shiftRightSize) % 8))
	}

	return dFinal
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
