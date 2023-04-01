package text

import (
	"context"
	"fmt"
	"github.com/netsampler/goflow2/format"
)

type TextDriver struct {
}

func (d *TextDriver) Prepare() error {
	return nil
}

func (d *TextDriver) Init(context.Context) error {
	return nil
}

func (d *TextDriver) Format(data interface{}) ([]byte, []byte, error) {
	var key []byte
	if dataIf, ok := data.(interface{ Key() []byte }); ok {
		key = dataIf.Key()
	}
	if dataIf, ok := data.(interface{ String() string }); ok {
		return key, []byte(dataIf.String()), nil
	}
	return nil, nil, fmt.Errorf("message is not serializable as string")
}

func init() {
	d := &TextDriver{}
	format.RegisterFormatDriver("text", d)
}
