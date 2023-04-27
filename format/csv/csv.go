package csv

import (
	"github.com/netsampler/goflow2/v2/format"
	protoproducer "github.com/netsampler/goflow2/v2/producer/proto"
)

type CSVDriver struct {
}

func (d *CSVDriver) Prepare() error {
	return nil
}

func (d *CSVDriver) Init() error {
	return nil
}

func (d *CSVDriver) Format(data interface{}) ([]byte, []byte, error) {
	var key []byte
	if dataIf, ok := data.(interface{ Key() []byte }); ok {
		key = dataIf.Key()
	}
	if dataIf, ok := data.(protoproducer.ProtoProducerMessage); ok {
		csv := []byte(dataIf.FormatMessageReflectCustom("", "", "", "", false, "FLOW"))
		return key, csv, nil
	}
	return key, nil, format.ErrorNoSerializer
}

func init() {
	d := &CSVDriver{}
	format.RegisterFormatDriver("csv", d)
}
