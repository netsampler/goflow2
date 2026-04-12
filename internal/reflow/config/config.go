package config

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type FlagConfig struct {
	ConfigPath string
	LogLevel   string
	LogFormat  string
}

type Config struct {
	LogLevel   string           `yaml:"-"`
	LogFormat  string           `yaml:"-"`
	Source     SourceConfig     `yaml:"source"`
	Processor  ProcessorConfig  `yaml:"processor"`
	Aggregator AggregatorConfig `yaml:"aggregator"`
	Encoder    EncoderConfig    `yaml:"encoder"`
	Sink       SinkConfig       `yaml:"sink"`
}

type SourceConfig struct {
	Network      string     `yaml:"network"`
	Address      string     `yaml:"address"`
	Interface    string     `yaml:"interface"`
	SnapLen      int        `yaml:"snaplen"`
	SampleEvery  int        `yaml:"sample_every"`
	SampleOffset int        `yaml:"sample_offset"`
	Type         string     `yaml:"type"`
	JSON         JSONConfig `yaml:"json"`
}

type ProcessorConfig struct {
	Type    string                 `yaml:"type"`
	Workers int                    `yaml:"workers"`
	Builtin BuiltinProcessorConfig `yaml:"builtin"`
}

type BuiltinProcessorConfig struct {
	DropMessage bool `yaml:"drop_message"`
	DropPayload bool `yaml:"drop_payload"`
}

type AggregatorConfig struct {
	Type          string   `yaml:"type"`
	FlushInterval int      `yaml:"flush_interval_ms"`
	KeyFields     []string `yaml:"key_fields"`
}

type EncoderConfig struct {
	Type             string      `yaml:"type"`
	Workers          int         `yaml:"workers"`
	MaxDatagramBytes int         `yaml:"max_datagram_bytes"`
	Batch            BatchConfig `yaml:"batch"`
	JSON             JSONConfig  `yaml:"json"`
	SFlow            SFlowConfig `yaml:"sflow"`
}

type JSONConfig struct {
	Flavor string `yaml:"flavor"`
}

type BatchConfig struct {
	Enabled       bool `yaml:"enabled"`
	MaxRecords    int  `yaml:"max_records"`
	MaxBytes      int  `yaml:"max_bytes"`
	FlushInterval int  `yaml:"flush_interval_ms"`
}

type SFlowConfig struct {
	AgentIP       string               `yaml:"agent_ip"`
	AllowTruncate bool                 `yaml:"allow_truncate"`
	BatchOver     SFlowBatchOverConfig `yaml:"batch_over"`
}

type SFlowBatchOverConfig struct {
	AgentIP        *bool `yaml:"agent_ip"`
	SubAgentID     *bool `yaml:"sub_agent_id"`
	SequenceNumber *bool `yaml:"sequence_number"`
	Uptime         *bool `yaml:"uptime"`
}

type SinkConfig struct {
	Type    string `yaml:"type"`
	Path    string `yaml:"path"`
	Address string `yaml:"address"`
	AgentIP string `yaml:"agent_ip"`
}

func BindFlags(fs *flag.FlagSet) (*FlagConfig, *bool) {
	cfg := &FlagConfig{}
	version := fs.Bool("v", false, "Print version")
	fs.StringVar(&cfg.ConfigPath, "config", "cmd/reflow/reflow.yaml", "Path to ReFlow YAML config")
	fs.StringVar(&cfg.LogLevel, "loglevel", "info", "Log level")
	fs.StringVar(&cfg.LogFormat, "logfmt", "text", "Log format (text or json)")
	return cfg, version
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load config %s: %w", path, err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}
	if err := cfg.setDefaults(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) setDefaults() error {
	if c.Source.Network == "" {
		c.Source.Network = "udp"
	}
	if c.Source.Network != "pcap_live" && c.Source.Address == "" {
		c.Source.Address = ":18080"
	}
	if c.Source.Network == "pcap_live" {
		if c.Source.Interface == "" {
			return fmt.Errorf("source.interface is required when source.network=pcap_live")
		}
		if c.Source.SnapLen <= 0 {
			c.Source.SnapLen = 65535
		}
		if c.Source.SampleEvery <= 0 {
			c.Source.SampleEvery = 1
		}
		if c.Source.SampleOffset < 0 || c.Source.SampleOffset >= c.Source.SampleEvery {
			return fmt.Errorf("source.sample_offset must be >= 0 and < source.sample_every")
		}
		if c.Source.Address == "" {
			c.Source.Address = c.Source.Interface
		}
		if c.Source.Type == "" {
			c.Source.Type = "bytes"
		}
	}
	if c.Processor.Type == "" {
		c.Processor.Type = "builtin"
	}
	if c.Processor.Workers <= 0 {
		c.Processor.Workers = 1
	}
	if c.Aggregator.Type == "" {
		c.Aggregator.Type = "none"
	}
	switch c.Aggregator.Type {
	case "none":
	case "flowstore_window":
		if c.Aggregator.FlushInterval <= 0 {
			c.Aggregator.FlushInterval = 10000
		}
		if len(c.Aggregator.KeyFields) == 0 {
			c.Aggregator.KeyFields = []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"}
		}
	default:
		return fmt.Errorf("unsupported aggregator.type %q", c.Aggregator.Type)
	}
	if c.Encoder.Type == "" {
		c.Encoder.Type = "json"
	}
	if c.Encoder.Workers <= 0 {
		c.Encoder.Workers = 1
	}
	if c.Encoder.MaxDatagramBytes <= 0 {
		c.Encoder.MaxDatagramBytes = 1400
	}
	defaultTrue(&c.Encoder.SFlow.BatchOver.AgentIP)
	defaultTrue(&c.Encoder.SFlow.BatchOver.SubAgentID)
	defaultTrue(&c.Encoder.SFlow.BatchOver.SequenceNumber)
	defaultTrue(&c.Encoder.SFlow.BatchOver.Uptime)
	if c.Encoder.Batch.MaxRecords < 0 {
		return fmt.Errorf("encoder.batch.max_records must be >= 0")
	}
	if c.Encoder.Batch.MaxBytes < 0 {
		return fmt.Errorf("encoder.batch.max_bytes must be >= 0")
	}
	if c.Encoder.Batch.FlushInterval < 0 {
		return fmt.Errorf("encoder.batch.flush_interval_ms must be >= 0")
	}
	switch c.Encoder.Type {
	case "json", "sflow":
	default:
		return fmt.Errorf("unsupported encoder.type %q", c.Encoder.Type)
	}
	if c.Sink.Type == "" {
		c.Sink.Type = "stdout"
	}
	switch c.Sink.Type {
	case "stdout", "file", "udp", "unixgram":
	default:
		return fmt.Errorf("unsupported sink.type %q", c.Sink.Type)
	}
	if c.Sink.Type == "file" && c.Sink.Path == "" {
		return fmt.Errorf("sink.path is required when sink.type=file")
	}
	if (c.Sink.Type == "udp" || c.Sink.Type == "unixgram") && c.Sink.Address == "" {
		return fmt.Errorf("sink.address is required when sink.type=%s", c.Sink.Type)
	}
	return nil
}

func defaultTrue(dst **bool) {
	if *dst != nil {
		return
	}
	v := true
	*dst = &v
}
