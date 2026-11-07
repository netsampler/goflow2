package config

import "fmt"

// ConntrackSourceConfig describes a local kernel conntrack table and shared cache.
// The source runs in ReFlow's network namespace; zone selects a single table.
type ConntrackSourceConfig struct {
	Name             string `yaml:"name"`
	Zone             uint16 `yaml:"zone,omitempty"`
	MaxEntries       int    `yaml:"max_entries,omitempty"`
	ResyncIntervalMS int    `yaml:"resync_interval_ms,omitempty"`
	ReceiveBuffer    int    `yaml:"receive_buffer,omitempty"`
	EmitEvents       bool   `yaml:"emit_events,omitempty"`
}

// ConntrackProcessorConfig selects the named input used to enrich local packets.
type ConntrackProcessorConfig struct {
	Input     string   `yaml:"input,omitempty"`
	TupleView string   `yaml:"tuple_view,omitempty"`
	Networks  []string `yaml:"networks,omitempty"`
}

func NormalizeConntrackSource(src *SourceConfig) error {
	c := &src.Conntrack
	if c.Name == "" {
		return fmt.Errorf("conntrack input requires conntrack.name")
	}
	if c.MaxEntries == 0 {
		c.MaxEntries = 65536
	}
	if c.ResyncIntervalMS == 0 {
		c.ResyncIntervalMS = 30000
	}
	if c.ReceiveBuffer == 0 {
		c.ReceiveBuffer = 4 * 1024 * 1024
	}
	if c.MaxEntries < 1 || c.ResyncIntervalMS < 1000 || c.ReceiveBuffer < 1 {
		return fmt.Errorf("conntrack max_entries and receive_buffer must be positive; resync_interval_ms must be >= 1000")
	}
	src.Type = "conntrack"
	src.Address = c.Name
	return nil
}
