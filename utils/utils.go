package utils

import (
	"gopkg.in/yaml.v2"
	"io"

	"github.com/netsampler/goflow2/producer"
)

type ProducerConfig *producer.ProducerConfig

func LoadMapping(f io.Reader) (ProducerConfig, error) {
	config := &producer.ProducerConfig{}
	dec := yaml.NewDecoder(f)
	err := dec.Decode(config)
	return config, err
}
