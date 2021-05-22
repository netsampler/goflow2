package protobuf

import (
	"context"
	"flag"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/netsampler/goflow2/format"
	"reflect"
	"strings"
)

var (
	fieldsVar string
	fields    []string // Hashing fields
)

type ProtobufDriver struct {
	fixedLen bool
}

func (d *ProtobufDriver) Prepare() error {
	flag.StringVar(&fieldsVar, "format.hash", "SamplerAddress", "List of fields to do hashing, separated by commas")
	flag.BoolVar(&d.fixedLen, "format.protobuf.fixedlen", false, "Prefix the protobuf with message length")
	return nil
}

func ManualInit() error {
	fields = strings.Split(fieldsVar, ",")
	return nil
}

func (d *ProtobufDriver) Init(context.Context) error {
	return ManualInit()
}

func (d *ProtobufDriver) Format(data interface{}) ([]byte, []byte, error) {
	msg, ok := data.(proto.Message)
	if !ok {
		return nil, nil, fmt.Errorf("message is not protobuf")
	}
	key := HashProtoLocal(msg)

	if !d.fixedLen {
		b, err := proto.Marshal(msg)
		return []byte(key), b, err
	} else {
		buf := proto.NewBuffer([]byte{})
		err := buf.EncodeMessage(msg)
		return []byte(key), buf.Bytes(), err
	}
}

func init() {
	d := &ProtobufDriver{}
	format.RegisterFormatDriver("pb", d)
}

func HashProtoLocal(msg interface{}) string {
	return HashProto(fields, msg)
}

func HashProto(fields []string, msg interface{}) string {
	var keyStr string

	if msg != nil {
		vfm := reflect.ValueOf(msg)
		vfm = reflect.Indirect(vfm)

		for _, kf := range fields {
			fieldValue := vfm.FieldByName(kf)
			if fieldValue.IsValid() {
				keyStr += fmt.Sprintf("%v-", fieldValue)
			}
		}
	}

	return keyStr
}
