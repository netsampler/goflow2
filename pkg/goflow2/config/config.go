package config

import (
	"flag"
	"fmt"
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

	StoreHTTPPath string

	TemplatesTTL time.Duration

	TemplatesSweepInterval  time.Duration
	TemplatesExtendOnAccess bool

	SamplingRatesTTL            time.Duration
	SamplingRatesSweepInterval  time.Duration
	SamplingRatesExtendOnAccess bool

	StoreJSONPath     string
	StoreJSONInterval time.Duration

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
	fs.StringVar(&cfg.StoreHTTPPath, "store.http.path", "/store", "Flowstore HTTP path")
	fs.DurationVar(&cfg.TemplatesTTL, "templates.ttl", 0, "NetFlow/IPFIX templates TTL (0 disables expiry)")
	fs.DurationVar(&cfg.TemplatesSweepInterval, "templates.sweep-interval", time.Minute, "NetFlow/IPFIX template expiry sweep interval")
	fs.BoolVar(&cfg.TemplatesExtendOnAccess, "templates.ttl.extend-on-access", false, "Extend template TTL on access")
	fs.StringVar(&cfg.StoreJSONPath, "store.json.path", "", "Shared flowstore JSON output path (empty disables persistence)")
	fs.DurationVar(&cfg.StoreJSONInterval, "store.json.interval", time.Second*10, "Shared flowstore JSON write interval")

	fs.DurationVar(&cfg.SamplingRatesTTL, "sampling.ttl", 0, "Sampling rates TTL (0 disables expiry)")
	fs.DurationVar(&cfg.SamplingRatesSweepInterval, "sampling.sweep-interval", time.Minute, "Sampling rates expiry sweep interval")
	fs.BoolVar(&cfg.SamplingRatesExtendOnAccess, "sampling.ttl.extend-on-access", false, "Extend sampling rate TTL on access")
	fs.StringVar(&cfg.MappingFile, "mapping", "", "Configuration file for custom mappings")

	return cfg
}

// LoadMapping reads a YAML mapping configuration.
func LoadMapping(r io.Reader) (*protoproducer.ProducerConfig, error) {
	config := &protoproducer.ProducerConfig{}
	dec := yaml.NewDecoder(r)
	err := dec.Decode(config)
	if err != nil {
		return nil, fmt.Errorf("decode mapping: %w", err)
	}
	return config, nil
}
