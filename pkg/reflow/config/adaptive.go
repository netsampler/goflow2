package config

import "fmt"

const (
	IPFIXSamplingSelector = "selector"
	IPFIXSamplingLegacy   = "legacy"
)

// IPFIXSamplingMode defaults adaptive IPFIX export to selector Options.
func (c *Config) IPFIXSamplingMode() string {
	mode := c.Encoder.TemplatedFlow.SamplingMode
	if mode == "" && c.Encoder.Type == "ipfix" {
		for _, source := range c.Sources {
			if source.Capture.Sampling.Adaptive.Enabled {
				return IPFIXSamplingSelector
			}
		}
	}
	return mode
}

// AdaptiveSamplingConfig adjusts a driver sampling divisor in response to capture
// delivery loss. It never changes the packets forwarded by the host.
type AdaptiveSamplingConfig struct {
	Enabled         bool   `yaml:"enabled,omitempty"`
	MinEvery        int    `yaml:"min_every,omitempty"`
	MaxEvery        int    `yaml:"max_every,omitempty"`
	IntervalMS      int    `yaml:"interval_ms,omitempty"`
	RecoveryAfterMS int    `yaml:"recovery_after_ms,omitempty"`
	IncreaseFactor  int    `yaml:"increase_factor,omitempty"`
	DecreaseFactor  int    `yaml:"decrease_factor,omitempty"`
	DropThreshold   uint64 `yaml:"drop_threshold,omitempty"`
}

func normalizeAdaptiveSampling(c *AdaptiveSamplingConfig, location string, every int) error {
	if !c.Enabled {
		return nil
	}
	if location != "kernel" && location != "callback" {
		return fmt.Errorf("adaptive sampling requires kernel or callback sampling")
	}
	if c.MinEvery == 0 {
		c.MinEvery = 1
	}
	if c.MaxEvery == 0 {
		c.MaxEvery = 256
	}
	if c.IntervalMS == 0 {
		c.IntervalMS = 1000
	}
	if c.RecoveryAfterMS == 0 {
		c.RecoveryAfterMS = 30000
	}
	if c.IncreaseFactor == 0 {
		c.IncreaseFactor = 4
	}
	if c.DecreaseFactor == 0 {
		c.DecreaseFactor = 2
	}
	if c.MinEvery < 1 || c.MaxEvery < c.MinEvery || uint64(c.MaxEvery) > uint64(^uint32(0)) || every < c.MinEvery || every > c.MaxEvery {
		return fmt.Errorf("adaptive sampling requires min_every <= every <= max_every within uint32")
	}
	if uint64(c.RecoveryAfterMS) > uint64((1<<63-1)/1000000) {
		return fmt.Errorf("adaptive sampling recovery_after_ms exceeds duration range")
	}
	if c.IntervalMS < 10 || c.IntervalMS > 3600000 || c.RecoveryAfterMS < c.IntervalMS || c.IncreaseFactor < 2 || c.DecreaseFactor < 2 || uint64(c.IncreaseFactor) > uint64(^uint32(0)) || uint64(c.DecreaseFactor) > uint64(^uint32(0)) {
		return fmt.Errorf("adaptive sampling requires interval_ms in 10..3600000, recovery_after_ms >= interval_ms, and factors in 2..4294967295")
	}
	return nil
}

// ValidateAdaptivePipeline prevents aggregation and output formats from losing
// the per-record divisor required to interpret dynamically sampled counters.
// Call after defaults and aggregator field normalization.
func (c *Config) ValidateAdaptivePipeline() error {
	switch c.Encoder.TemplatedFlow.SamplingMode {
	case "":
	case IPFIXSamplingSelector, IPFIXSamplingLegacy:
		if c.Encoder.Type != "ipfix" {
			return fmt.Errorf("encoder.templated_flow.sampling_mode requires IPFIX output")
		}
	default:
		return fmt.Errorf("encoder.templated_flow.sampling_mode must be selector or legacy")
	}
	networks := make(map[string]bool)
	selectorMode := c.IPFIXSamplingMode() == IPFIXSamplingSelector
	for i, source := range c.Sources {
		if selectorMode && IsCaptureNetwork(source.Network) &&
			max(source.Capture.SampleEvery, source.Capture.Sampling.Every) > 1 &&
			!source.Capture.Sampling.SelectsInDriver() {
			return fmt.Errorf("sources[%d]: systematic packet sampling requires IPFIX sampling_mode: legacy", i)
		}
		if source.Capture.Sampling.Adaptive.Enabled {
			networks[source.Network] = true
		}
	}
	if len(networks) == 0 {
		return nil
	}
	for i, agg := range c.Aggregators {
		if agg.Match["kind"] == "control" || agg.Match["control.type"] != "" {
			continue
		}
		if network := agg.Match["source.network"]; network != "" && !networks[network] {
			continue
		}
		if kind := agg.Match["record_kind"]; kind != "" && kind != "packet" {
			continue
		}
		if _, exists := agg.StaticFields["sampling_rate"]; exists {
			return fmt.Errorf("aggregators[%d]: adaptive sampling cannot use a static sampling_rate", i)
		}
		found := false
		if len(agg.Fields) == 0 {
			for _, key := range agg.KeyFields {
				found = found || key == "sampling_rate"
			}
			if agg.Passthrough {
				for _, fields := range [][]string{agg.Current, agg.First, agg.Sum, agg.Min, agg.Max, agg.And} {
					for _, field := range fields {
						found = found || field == "sampling_rate"
					}
				}
			}
		}
		for _, field := range agg.Fields {
			if field.Name == "sampling_rate" {
				if field.Role == "static" || (!agg.Passthrough && field.Role != "key") {
					return fmt.Errorf("aggregators[%d]: adaptive sampling requires key:sampling_rate", i)
				}
				found = true
			}
		}
		if agg.Passthrough {
			if !found && (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") {
				return fmt.Errorf("aggregators[%d]: adaptive sampling requires sampling_rate in the passthrough schema", i)
			}
			continue
		}
		if !found {
			return fmt.Errorf("aggregators[%d]: adaptive sampling requires key:sampling_rate", i)
		}
	}
	switch c.Encoder.Type {
	case "", "json":
		if c.Encoder.JSON.Flavor == "vendor" {
			return fmt.Errorf("adaptive sampling requires JSON canonical or goflow2v2 flavor to retain sampling_rate")
		}
		for _, field := range c.Encoder.JSON.DropFields {
			if field == "sampling_rate" {
				return fmt.Errorf("adaptive sampling cannot drop JSON sampling_rate")
			}
		}
	case "netflowv5", "pcap", "pcapng":
		return fmt.Errorf("adaptive sampling requires an output format retaining per-record sampling_rate, got %s", c.Encoder.Type)
	case "ipfix", "netflowv9":
		if fields := c.Encoder.TemplatedFlow.Data.Select; len(fields) > 0 {
			found := false
			for _, field := range fields {
				if field == "sampling_rate" {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("adaptive sampling requires sampling_rate in encoder.templated_flow.data.fields")
			}
		}
	}
	return nil
}
