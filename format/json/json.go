package json

import (
	"encoding/json"
	"fmt"

	"github.com/netsampler/goflow2/v2/format"
)

type JsonDriver struct {
}

func (d *JsonDriver) Prepare() error {
	return nil
}

func (d *JsonDriver) Init() error {
	return nil
}

func (d *JsonDriver) Format(data interface{}) ([]byte, []byte, error) {
	var key []byte
	if dataIf, ok := data.(interface{ Key() []byte }); ok {
		key = dataIf.Key()
	}
	fmt.Printf("stack: %v\n", data)
	output, err := json.Marshal(data)
	fmt.Println(err)
	return key, output, err
}

func init() {
	d := &JsonDriver{}
	format.RegisterFormatDriver("json", d)
}
