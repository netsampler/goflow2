package builder

import (
	"fmt"
	"os"

	"github.com/netsampler/goflow2/v3/format"
	"github.com/netsampler/goflow2/v3/pkg/goflow2/config"
	"github.com/netsampler/goflow2/v3/producer"
	protoproducer "github.com/netsampler/goflow2/v3/producer/proto"
	rawproducer "github.com/netsampler/goflow2/v3/producer/raw"
	"github.com/netsampler/goflow2/v3/transport"
)

// BuildFormatter resolves a formatter by name.
func BuildFormatter(name string) (format.FormatInterface, error) {
	formatter, err := format.FindFormat(name)
	if err != nil {
		return nil, fmt.Errorf("build formatter %s: %w", name, err)
	}
	return formatter, nil
}

// BuildTransport resolves a transport by name.
func BuildTransport(name string) (*transport.Transport, error) {
	t, err := transport.FindTransport(name)
	if err != nil {
		return nil, fmt.Errorf("build transport %s: %w", name, err)
	}
	return t, nil
}

// BuildProducer resolves a producer based on configuration.
func BuildProducer(cfg *config.Config) (producer.ProducerInterface, error) {
	switch cfg.Produce {
	case "sample":
		var cfgProducer *protoproducer.ProducerConfig
		if cfg.MappingFile != "" {
			f, err := os.Open(cfg.MappingFile)
			if err != nil {
				return nil, fmt.Errorf("load mapping %s: open: %w", cfg.MappingFile, err)
			}
			cfgProducer, err = config.LoadMapping(f)
			_ = f.Close()
			if err != nil {
				return nil, fmt.Errorf("load mapping %s: decode: %w", cfg.MappingFile, err)
			}
		}

		cfgm, err := cfgProducer.Compile()
		if err != nil {
			return nil, fmt.Errorf("compile mapping: %w", err)
		}

		return protoproducer.CreateProtoProducer(cfgm, protoproducer.CreateSamplingSystem)
	case "raw":
		return &rawproducer.RawProducer{}, nil
	default:
		return nil, fmt.Errorf("producer does not exist: %s", cfg.Produce)
	}
}
