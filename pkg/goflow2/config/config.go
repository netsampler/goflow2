package config

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/netsampler/goflow2/v2/format"
	protoproducer "github.com/netsampler/goflow2/v2/producer/proto"
	"github.com/netsampler/goflow2/v2/transport"
	"gopkg.in/yaml.v3"
)

// Config holds configuration for the GoFlow2 application.
type Config struct {
	ListenAddresses string

	LogLevel string
	LogFmt   string

	Produce   string
	Format    string
	Transport string

	ErrCnt int
	ErrInt time.Duration

	Addr string

	TemplatePath string

	MappingFile string
}

// BindFlags registers configuration flags and returns a Config.
func BindFlags(fs *flag.FlagSet) *Config {
	cfg := &Config{}

	fs.StringVar(&cfg.ListenAddresses, "listen", "sflow://:6343,netflow://:2055", "listen addresses")
	fs.StringVar(&cfg.LogLevel, "loglevel", "info", "Log level")
	fs.StringVar(&cfg.LogFmt, "logfmt", "normal", "Log formatter")
	fs.StringVar(&cfg.Produce, "produce", "sample", "Producer method (sample or raw)")
	fs.StringVar(&cfg.Format, "format", "json", fmt.Sprintf("Choose the format (available: %s)", strings.Join(format.GetFormats(), ", ")))
	fs.StringVar(&cfg.Transport, "transport", "file", fmt.Sprintf("Choose the transport (available: %s)", strings.Join(transport.GetTransports(), ", ")))
	fs.IntVar(&cfg.ErrCnt, "err.cnt", 10, "Maximum errors per batch for muting")
	fs.DurationVar(&cfg.ErrInt, "err.int", time.Second*10, "Maximum errors interval for muting")
	fs.StringVar(&cfg.Addr, "addr", ":8080", "HTTP server address")
	fs.StringVar(&cfg.TemplatePath, "templates.path", "/templates", "NetFlow/IPFIX templates list")
	fs.StringVar(&cfg.MappingFile, "mapping", "", "Configuration file for custom mappings")

	return cfg
}

// LoadMapping reads a YAML mapping configuration.
func LoadMapping(r io.Reader) (*protoproducer.ProducerConfig, error) {
	config := &protoproducer.ProducerConfig{}
	dec := yaml.NewDecoder(r)
	err := dec.Decode(config)
	return config, err
}
