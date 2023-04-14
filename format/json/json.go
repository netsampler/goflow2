package json

import (
	"context"
	//"fmt"
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
	if dataIf, ok := data.(interface{ MarshalJSON() ([]byte, error) }); ok {
		d, err := dataIf.MarshalJSON()
		return key, d, err
	}
	output, err := json.Marshal(data)
	return key, output, err
	//return nil, nil, fmt.Errorf("message is not serializable in json")
}

func init() {
	d := &JsonDriver{}
	format.RegisterFormatDriver("json", d)
}
