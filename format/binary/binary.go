package binary

import (
	"encoding"

	"github.com/netsampler/goflow2/v2/format"
)

type BinaryDriver struct {
}

func (d *BinaryDriver) Prepare() error {
	return nil
}

func (d *BinaryDriver) Init() error {
	return nil
}

func (d *BinaryDriver) Format(data interface{}) ([]byte, []byte, error) {
	var key []byte
	if dataIf, ok := data.(interface{ Key() []byte }); ok {
		key = dataIf.Key()
	}
	if dataIf, ok := data.(encoding.BinaryMarshaler); ok {
		text, err := dataIf.MarshalBinary()
		return key, text, err
	}
	return key, nil, format.ErrNoSerializer
}

func init() {
	d := &BinaryDriver{}
	format.RegisterFormatDriver("bin", d)
}
