package producer

import (
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

type MapConfigBase struct {
	Destination string
	Endianness  EndianType
	ProtoIndex  int
	ProtoType   string
	ProtoArray  bool
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
						DecodeUNumberLE(v, item.Interface())
					} else {
						DecodeUNumber(v, item.Interface())
					}
				} else if IsUInt(typeDest.Elem().Kind()) {
					if cfg.Endianness == LittleEndian {
						DecodeUNumberLE(v, item.Interface())
					} else {
						DecodeUNumber(v, item.Interface())
					}
				}

				itemi := reflect.Indirect(item)
				tmpFieldValue := reflect.Append(fieldValue, itemi)
				fieldValue.Set(tmpFieldValue)
			}

		} else if fieldValueAddr.IsValid() {
			if IsUInt(typeDest.Kind()) {
				if cfg.Endianness == LittleEndian {
					DecodeUNumberLE(v, fieldValueAddr.Interface())
				} else {
					DecodeUNumber(v, fieldValueAddr.Interface())
				}
			} else if IsInt(typeDest.Kind()) {
				if cfg.Endianness == LittleEndian {
					DecodeUNumberLE(v, fieldValueAddr.Interface())
				} else {
					DecodeUNumber(v, fieldValueAddr.Interface())
				}
			}

		}
	} else if cfg.ProtoIndex > 0 {
	}
	return nil
}
