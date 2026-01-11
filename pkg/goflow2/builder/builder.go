package builder

import (
	"fmt"
	"os"

	"github.com/netsampler/goflow2/v2/format"
	"github.com/netsampler/goflow2/v2/pkg/goflow2/config"
	"github.com/netsampler/goflow2/v2/producer"
	protoproducer "github.com/netsampler/goflow2/v2/producer/proto"
	rawproducer "github.com/netsampler/goflow2/v2/producer/raw"
	"github.com/netsampler/goflow2/v2/transport"
)

// BuildFormatter resolves a formatter by name.
func BuildFormatter(name string) (format.FormatInterface, error) {
	return format.FindFormat(name)
}

// BuildTransport resolves a transport by name.
func BuildTransport(name string) (*transport.Transport, error) {
	return transport.FindTransport(name)
}

// BuildProducer resolves a producer based on configuration.
func BuildProducer(cfg *config.Config) (producer.ProducerInterface, error) {
	switch cfg.Produce {
	case "sample":
		var cfgProducer *protoproducer.ProducerConfig
		if cfg.MappingFile != "" {
			f, err := os.Open(cfg.MappingFile)
			if err != nil {
				return nil, err
			}
			cfgProducer, err = config.LoadMapping(f)
			_ = f.Close()
			if err != nil {
				return nil, err
			}
		}

		if cfgProducer == nil {
			cfgProducer = &protoproducer.ProducerConfig{}
		}
		cfgProducer.SamplingRateFallback = cfg.SamplingRateFallback

		cfgm, err := cfgProducer.Compile()
		if err != nil {
			return nil, err
		}

		return protoproducer.CreateProtoProducer(cfgm, protoproducer.CreateSamplingSystem)
	case "raw":
		return &rawproducer.RawProducer{}, nil
	default:
		return nil, fmt.Errorf("producer does not exist: %s", cfg.Produce)
	}
}
