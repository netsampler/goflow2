package text

import (
	"context"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/netsampler/goflow2/format"
	"github.com/netsampler/goflow2/format/common"
)

type TextDriver struct {
}

func (d *TextDriver) Prepare() error {
	common.HashFlag()
	return nil
}

func (d *TextDriver) Init(context.Context) error {
	return common.ManualHashInit()
}

func (d *TextDriver) Format(data interface{}) ([]byte, []byte, error) {
	msg, ok := data.(proto.Message)
	if !ok {
		return nil, nil, fmt.Errorf("message is not protobuf")
	}

	key := common.HashProtoLocal(msg)
	return []byte(key), []byte(common.FormatMessageReflectText(msg, "")), nil
}

func init() {
	d := &TextDriver{}
	format.RegisterFormatDriver("text", d)
}
