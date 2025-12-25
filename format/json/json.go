// Package json implements JSON output formatting.
package json

import (
	"encoding/json"

	"github.com/netsampler/goflow2/v2/format"
)

// JsonDriver formats flow messages using JSON encoding.
type JsonDriver struct{}

// Prepare performs any one-time setup for the driver.
func (d *JsonDriver) Prepare() error {
	return nil
}

// Init finalizes runtime configuration for the driver.
func (d *JsonDriver) Init() error {
	return nil
}

// Format encodes the input payload as JSON, preserving a Key when available.
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
