// Package binary implements binary output formatting using encoding.BinaryMarshaler.
package binary

import (
	"encoding"

	"github.com/netsampler/goflow2/v2/format"
)

// BinaryDriver formats flow messages via MarshalBinary.
type BinaryDriver struct{}

// Prepare performs any one-time setup for the driver.
func (d *BinaryDriver) Prepare() error {
	return nil
}

// Init finalizes runtime configuration for the driver.
func (d *BinaryDriver) Init() error {
	return nil
}

// Format marshals the payload via encoding.BinaryMarshaler, preserving a Key when available.
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
