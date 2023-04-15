package json

import (
	"context"
	"encoding/json"

	"github.com/netsampler/goflow2/format"
)

type JsonDriver struct {
}

func (d *JsonDriver) Prepare() error {
	return nil
}

func (d *JsonDriver) Init(context.Context) error {
	return nil
}

func (d *JsonDriver) Format(data interface{}) ([]byte, []byte, error) {
	var key []byte
	if dataIf, ok := data.(interface{ Key() []byte }); ok {
		key = dataIf.Key()
	}
	output, err := json.Marshal(data)
	return key, output, err
}

func init() {
	d := &JsonDriver{}
	format.RegisterFormatDriver("json", d)
}
