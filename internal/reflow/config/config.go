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
	LogLevel   string            `yaml:"-"`
	LogFormat  string            `yaml:"-"`
	Source     SourceConfig      `yaml:"source"`
	Transforms []TransformConfig `yaml:"transforms"`
	Output     OutputConfig      `yaml:"output"`
}

type SourceConfig struct {
	Network string `yaml:"network"`
	Address string `yaml:"address"`
	Frame   string `yaml:"frame"`
}

type TransformConfig struct {
	Type   string         `yaml:"type"`
	Fields map[string]any `yaml:"fields"`
}

type OutputConfig struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
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
	if c.Source.Address == "" {
		c.Source.Address = ":18080"
	}
	if c.Source.Frame == "" {
		c.Source.Frame = "datagram"
	}
	if c.Source.Frame != "datagram" {
		return fmt.Errorf("unsupported source.frame %q", c.Source.Frame)
	}
	if c.Output.Type == "" {
		c.Output.Type = "stdout"
	}
	switch c.Output.Type {
	case "stdout", "file":
	default:
		return fmt.Errorf("unsupported output.type %q", c.Output.Type)
	}
	if c.Output.Type == "file" && c.Output.Path == "" {
		return fmt.Errorf("output.path is required when output.type=file")
	}
	return nil
}
