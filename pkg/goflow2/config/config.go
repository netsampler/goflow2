package config

import (
	"flag"
	"io"
	"time"

	protoproducer "github.com/netsampler/goflow2/v3/producer/proto"
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
	TemplatesTTL time.Duration

	TemplatesSweepInterval  time.Duration
	TemplatesExtendOnAccess bool
	TemplatesJSONPath       string
	TemplatesJSONInterval   time.Duration

	MappingFile string
}

// BindFlags registers configuration flags and returns a Config.
func BindFlags(fs *flag.FlagSet) *Config {
	cfg := &Config{}

	fs.StringVar(&cfg.ListenAddresses, "listen", "sflow://:6343,netflow://:2055", "listen addresses")
	fs.StringVar(&cfg.Produce, "produce", "sample", "Producer method (sample or raw)")
	BindCommonFlags(fs, &cfg.LogLevel, &cfg.LogFmt, &cfg.Format, &cfg.Transport)
	fs.IntVar(&cfg.ErrCnt, "err.cnt", 10, "Maximum errors per batch for muting")
	fs.DurationVar(&cfg.ErrInt, "err.int", time.Second*10, "Maximum errors interval for muting")
	fs.StringVar(&cfg.Addr, "addr", ":8080", "HTTP server address")
	fs.StringVar(&cfg.TemplatePath, "templates.path", "/templates", "NetFlow/IPFIX templates list")
	fs.DurationVar(&cfg.TemplatesTTL, "templates.ttl", 0, "NetFlow/IPFIX templates TTL (0 disables expiry)")
	fs.DurationVar(&cfg.TemplatesSweepInterval, "templates.sweep-interval", time.Minute, "NetFlow/IPFIX template sweep interval (expiry + empty cleanup)")
	fs.BoolVar(&cfg.TemplatesExtendOnAccess, "templates.ttl.extend-on-access", false, "Extend template TTL on access")
	fs.StringVar(&cfg.TemplatesJSONPath, "templates.json.path", "", "NetFlow/IPFIX templates JSON output path (empty disables persistence)")
	fs.DurationVar(&cfg.TemplatesJSONInterval, "templates.json.interval", time.Second*10, "NetFlow/IPFIX templates JSON write interval")
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
