package text

import (
	"context"
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/netsampler/goflow2/format"
	"github.com/netsampler/goflow2/format/common"
)

type CSVDriver struct {
}

func (d *CSVDriver) Prepare() error {
	common.HashFlag()
	common.SelectorFlag()
	return nil
}

func (d *CSVDriver) Init(context.Context) error {
	err := common.ManualHashInit()
	if err != nil {
		return err
	}
	return common.ManualSelectorInit()
}

func (d *CSVDriver) Format(data interface{}) ([]byte, []byte, error) {
	msg, ok := data.(proto.Message)
	if !ok {
		return nil, nil, fmt.Errorf("message is not protobuf")
	}

	key := common.HashProtoLocal(msg)
	return []byte(key), []byte(common.FormatMessageReflectCustom(msg, "", "", ",", "", false, "FLOW")), nil
}

func init() {
	d := &CSVDriver{}
	format.RegisterFormatDriver("csv", d)
}
