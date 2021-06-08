package json

import (
	"context"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/netsampler/goflow2/format"
	"github.com/netsampler/goflow2/format/common"
)

type JsonDriver struct {
}

func (d *JsonDriver) Prepare() error {
	common.HashFlag()
	return nil
}

func (d *JsonDriver) Init(context.Context) error {
	return common.ManualHashInit()
}

func (d *JsonDriver) Format(data interface{}) ([]byte, []byte, error) {
	msg, ok := data.(proto.Message)
	if !ok {
		return nil, nil, fmt.Errorf("message is not protobuf")
	}

	key := common.HashProtoLocal(msg)
	return []byte(key), []byte(common.FormatMessageReflect(msg, "")), nil
}

func init() {
	d := &JsonDriver{}
	format.RegisterFormatDriver("json", d)
}
