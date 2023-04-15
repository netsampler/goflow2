package binary

import (
	"context"
	"encoding"
	"fmt"

	"github.com/netsampler/goflow2/format"
)

type BinaryDriver struct {
}

func (d *BinaryDriver) Prepare() error {
	return nil
}

func (d *BinaryDriver) Init(context.Context) error {
	return nil
}

func (d *BinaryDriver) Format(data interface{}) ([]byte, []byte, error) {
	var key []byte
	if dataIf, ok := data.(interface{ Key() []byte }); ok {
		key = dataIf.Key()
	}
	if dataIf, ok := data.(encoding.BinaryMarshaler); ok {
		txt, err := dataIf.MarshalBinary()
		return key, txt, err
	}
	return key, nil, fmt.Errorf("message is not serializable as binary")
}

func init() {
	d := &BinaryDriver{}
	format.RegisterFormatDriver("bin", d)
}
