// Package text implements text output formatting.
package text

import (
	"encoding"

	"github.com/netsampler/goflow2/v2/format"
)

// TextDriver formats flow messages via text or string serializers.
type TextDriver struct{}

// Prepare performs any one-time setup for the driver.
func (d *TextDriver) Prepare() error {
	return nil
}

// Init finalizes runtime configuration for the driver.
func (d *TextDriver) Init() error {
	return nil
}

// Format marshals the payload via TextMarshaler or Stringer, preserving a Key when available.
func (d *TextDriver) Format(data interface{}) ([]byte, []byte, error) {
	var key []byte
	if dataIf, ok := data.(interface{ Key() []byte }); ok {
		key = dataIf.Key()
	}
	if dataIf, ok := data.(encoding.TextMarshaler); ok {
		text, err := dataIf.MarshalText()
		return key, text, err
	}
	if dataIf, ok := data.(interface{ String() string }); ok {
		return key, []byte(dataIf.String()), nil
	}
	return key, nil, format.ErrNoSerializer
}

func init() {
	d := &TextDriver{}
	format.RegisterFormatDriver("text", d)
}
