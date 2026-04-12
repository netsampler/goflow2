package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type FlagConfig struct {
	ConfigPath string
	LogLevel   string
	LogFormat  string
}

type Config struct {
	LogLevel    string             `yaml:"-"`
	LogFormat   string             `yaml:"-"`
	Sources     []SourceConfig     `yaml:"sources"`
	Processor   ProcessorConfig    `yaml:"processor"`
	Aggregators []AggregatorConfig `yaml:"aggregators"`
	Encoder     EncoderConfig      `yaml:"encoder"`
	Sink        SinkConfig         `yaml:"sink"`
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
	DropMessage          bool `yaml:"drop_message"`
	DropPayload          bool `yaml:"drop_payload"`
	DisablePacketMapping bool `yaml:"disable_packet_mapping"`
	TruncatePacketBytes  int  `yaml:"truncate_packet_bytes"`
	BuildPseudoPacket    bool `yaml:"build_pseudo_packet"`
}

type AggregatorConfig struct {
	Enabled bool   `yaml:"enabled"`
	Stream  string `yaml:"stream"`
	// Window controls bucket closure based on activity and age.
	Window AggregatorWindowConfig `yaml:"window"`
	// Periodic controls snapshot-style exports of current bucket state.
	Periodic     AggregatorPeriodicConfig `yaml:"periodic"`
	KeyFields    []string                 `yaml:"key_fields"`
	Sum          []string                 `yaml:"sum"`
	First        []string                 `yaml:"first"`
	Current      []string                 `yaml:"current"`
	Match        map[string]string        `yaml:"match"`
	TemplateID   uint16                   `yaml:"template_id"`
	StaticFields map[string]any           `yaml:"static_fields"`

	// Deprecated compatibility knobs. They are still parsed so older configs keep
	// loading, then mapped into the explicit window/periodic sections.
	ResetInterval    int `yaml:"reset_interval_ms"`
	PeriodicInterval int `yaml:"periodic_interval_ms"`
}

type AggregatorWindowConfig struct {
	// IdleFlushAfter exports and removes a bucket after this much time without updates.
	IdleFlushAfter int `yaml:"idle_flush_after_ms"`
	// MaxFlushAfter exports and removes a bucket once it reaches this lifetime.
	MaxFlushAfter int `yaml:"max_flush_after_ms"`
	// IdleEraseAfter removes stale buckets without exporting them.
	IdleEraseAfter int `yaml:"idle_erase_after_ms"`
}

type AggregatorPeriodicConfig struct {
	// Every emits periodic snapshots of current bucket state.
	Every int `yaml:"every_ms"`
	// ResetBuckets turns periodic export into "emit and clear" instead of "emit snapshot and keep".
	ResetBuckets bool `yaml:"reset_buckets"`
}

type EncoderConfig struct {
	Type                  string          `yaml:"type"`
	Workers               int             `yaml:"workers"`
	TemplateBaseID        uint16          `yaml:"template_base_id"`
	OptionsTemplateBaseID uint16          `yaml:"options_template_base_id"`
	ObservationDomainID   uint32          `yaml:"observation_domain_id"`
	MaxDatagramBytes      int             `yaml:"max_datagram_bytes"`
	AllowTruncate         bool            `yaml:"allow_truncate"`
	TemplateRefresh       int             `yaml:"template_refresh_ms"`
	OptionsRefresh        int             `yaml:"options_refresh_ms"`
	Batch                 BatchConfig     `yaml:"batch"`
	TFlowData             TFlowDataConfig `yaml:"tflow_data"`
	JSON                  JSONConfig      `yaml:"json"`
	Protobuf              ProtobufConfig  `yaml:"protobuf"`
	SFlow                 SFlowConfig     `yaml:"sflow"`
}

type JSONConfig struct {
	Flavor     string   `yaml:"flavor"`
	DropFields []string `yaml:"drop_fields"`
}

type ProtobufConfig struct {
	Flavor         string `yaml:"flavor"`
	LengthPrefixed bool   `yaml:"length_prefixed"`
}

type BatchConfig struct {
	Enabled       bool `yaml:"enabled"`
	MaxRecords    int  `yaml:"max_records"`
	MaxBytes      int  `yaml:"max_bytes"`
	FlushInterval int  `yaml:"flush_interval_ms"`
}

type SFlowConfig struct {
	AgentIP       string               `yaml:"agent_ip"`
	CounterFormat string               `yaml:"counter_format"`
	BatchOver     SFlowBatchOverConfig `yaml:"batch_over"`
}

type SFlowBatchOverConfig struct {
	AgentIP        *bool `yaml:"agent_ip"`
	SubAgentID     *bool `yaml:"sub_agent_id"`
	SequenceNumber *bool `yaml:"sequence_number"`
	Uptime         *bool `yaml:"uptime"`
}

type TFlowDataConfig struct {
	Select     []string                        `yaml:"fields"`
	FieldsPath string                          `yaml:"fields_path"`
	Catalog    map[string]IPFIXFieldDefinition `yaml:"-"`
	Overrides  map[string]IPFIXFieldDefinition `yaml:"overrides"`
}

type IPFIXFieldDefinition struct {
	Name             string `yaml:"name"`
	ID               uint16 `yaml:"id"`
	PEN              uint32 `yaml:"pen"`
	Length           uint16 `yaml:"length"`
	Type             string `yaml:"type"`
	Format           string `yaml:"format"`
	NetFlowV9ID      uint16 `yaml:"netflow_v9_id"`
	EnterpriseScoped bool   `yaml:"enterprise_scoped"`
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
	if err := cfg.setDefaults(path); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) setDefaults(configPath string) error {
	if len(c.Sources) == 0 {
		return fmt.Errorf("sources must contain at least one source")
	}
	for i := range c.Sources {
		if err := applySourceDefaults(&c.Sources[i]); err != nil {
			return fmt.Errorf("sources[%d]: %w", i, err)
		}
	}
	if c.Processor.Type == "" {
		c.Processor.Type = "builtin"
	}
	if c.Processor.Workers <= 0 {
		c.Processor.Workers = 1
	}
	if c.Processor.Builtin.TruncatePacketBytes < 0 {
		return fmt.Errorf("processor.builtin.truncate_packet_bytes must be >= 0")
	}
	if len(c.Aggregators) > 0 {
		for i := range c.Aggregators {
			applyAggregatorCompatibility(&c.Aggregators[i])
			if err := validateAggregatorConfig(c.Aggregators[i]); err != nil {
				return fmt.Errorf("aggregators[%d]: %w", i, err)
			}
			defaultAggregateFields(&c.Aggregators[i])
		}
	}
	if err := c.loadFlowDataCatalog(configPath); err != nil {
		return err
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
	if (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") && c.Encoder.TemplateBaseID == 0 {
		c.Encoder.TemplateBaseID = 256
	}
	if (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") && c.Encoder.OptionsTemplateBaseID == 0 {
		c.Encoder.OptionsTemplateBaseID = 1024
	}
	if c.Encoder.TemplateRefresh < 0 {
		return fmt.Errorf("encoder.template_refresh_ms must be >= 0")
	}
	if c.Encoder.OptionsRefresh < 0 {
		return fmt.Errorf("encoder.options_refresh_ms must be >= 0")
	}
	if (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") && c.Encoder.TemplateRefresh == 0 {
		c.Encoder.TemplateRefresh = 60000
	}
	if (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") && c.Encoder.OptionsRefresh == 0 {
		c.Encoder.OptionsRefresh = 30000
	}
	defaultTrue(&c.Encoder.SFlow.BatchOver.AgentIP)
	defaultTrue(&c.Encoder.SFlow.BatchOver.SubAgentID)
	defaultTrue(&c.Encoder.SFlow.BatchOver.SequenceNumber)
	defaultTrue(&c.Encoder.SFlow.BatchOver.Uptime)
	switch c.Encoder.SFlow.CounterFormat {
	case "", "standard":
		c.Encoder.SFlow.CounterFormat = "standard"
	case "expanded":
	default:
		return fmt.Errorf("unsupported encoder.sflow.counter_format %q", c.Encoder.SFlow.CounterFormat)
	}
	switch c.Encoder.Protobuf.Flavor {
	case "", "canonical":
		c.Encoder.Protobuf.Flavor = "canonical"
	case "goflow2v2":
	default:
		return fmt.Errorf("unsupported encoder.protobuf.flavor %q", c.Encoder.Protobuf.Flavor)
	}
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
	case "json", "protobuf", "sflow", "ipfix", "netflowv9", "netflowv5":
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

func applySourceDefaults(src *SourceConfig) error {
	if src.Network == "" {
		src.Network = "udp"
	}
	if src.Network != "pcap_live" && src.Address == "" {
		src.Address = ":18080"
	}
	if src.Network == "pcap_live" {
		if src.Interface == "" {
			return fmt.Errorf("source.interface is required when source.network=pcap_live")
		}
		if src.SnapLen <= 0 {
			src.SnapLen = 65535
		}
		if src.SampleEvery <= 0 {
			src.SampleEvery = 1
		}
		if src.SampleOffset < 0 || src.SampleOffset >= src.SampleEvery {
			return fmt.Errorf("source.sample_offset must be >= 0 and < source.sample_every")
		}
		if src.Address == "" {
			src.Address = src.Interface
		}
		if src.Type == "" {
			src.Type = "bytes"
		}
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

func (c *Config) loadFlowDataCatalog(configPath string) error {
	if c.Encoder.TFlowData.FieldsPath == "" {
		c.Encoder.TFlowData.FieldsPath = "reflow-ipfix-fields.yaml"
	}
	if !filepath.IsAbs(c.Encoder.TFlowData.FieldsPath) {
		c.Encoder.TFlowData.FieldsPath = filepath.Join(filepath.Dir(configPath), c.Encoder.TFlowData.FieldsPath)
	}

	type ipfixCatalog struct {
		Fields map[string]IPFIXFieldDefinition `yaml:"fields"`
	}

	catalog := ipfixCatalog{}
	raw, err := os.ReadFile(c.Encoder.TFlowData.FieldsPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.Encoder.TFlowData.Catalog = mergeIPFIXFields(c.Encoder.TFlowData.Catalog, c.Encoder.TFlowData.Overrides)
			return nil
		}
		return fmt.Errorf("load tflow_data fields %s: %w", c.Encoder.TFlowData.FieldsPath, err)
	}
	if err := yaml.Unmarshal(raw, &catalog); err != nil {
		return fmt.Errorf("decode tflow_data fields %s: %w", c.Encoder.TFlowData.FieldsPath, err)
	}

	c.Encoder.TFlowData.Catalog = mergeIPFIXFields(catalog.Fields, c.Encoder.TFlowData.Catalog, c.Encoder.TFlowData.Overrides)
	return nil
}

func mergeIPFIXFields(sources ...map[string]IPFIXFieldDefinition) map[string]IPFIXFieldDefinition {
	merged := make(map[string]IPFIXFieldDefinition)
	for _, source := range sources {
		for key, def := range source {
			merged[key] = def
		}
	}
	return merged
}

func defaultAggregateFields(cfg *AggregatorConfig) {
	if cfg.Stream == "" {
		cfg.Stream = "flow_data"
	}
	if len(cfg.Sum) == 0 {
		cfg.Sum = []string{"bytes", "packets"}
	}
	if len(cfg.First) == 0 {
		cfg.First = []string{
			"agent_ip",
			"sub_agent_id",
			"source_id",
			"start_time_unix",
		}
	}
	if len(cfg.Current) == 0 {
		cfg.Current = []string{
			"agent_ip",
			"sub_agent_id",
			"source_id",
			"input_if",
			"output_if",
			"sampling_rate",
			"sample_pool",
			"drops",
			"end_time_unix",
		}
	}
}

func applyAggregatorCompatibility(cfg *AggregatorConfig) {
	if cfg.Window.IdleFlushAfter == 0 && cfg.ResetInterval > 0 {
		cfg.Window.IdleFlushAfter = cfg.ResetInterval
	}
	if cfg.Periodic.Every == 0 && cfg.PeriodicInterval > 0 {
		cfg.Periodic.Every = cfg.PeriodicInterval
	}
}

func validateAggregatorConfig(cfg AggregatorConfig) error {
	if cfg.Window.IdleFlushAfter < 0 {
		return fmt.Errorf("aggregator.window.idle_flush_after_ms must be >= 0")
	}
	if cfg.Window.MaxFlushAfter < 0 {
		return fmt.Errorf("aggregator.window.max_flush_after_ms must be >= 0")
	}
	if cfg.Window.IdleEraseAfter < 0 {
		return fmt.Errorf("aggregator.window.idle_erase_after_ms must be >= 0")
	}
	if cfg.Periodic.Every < 0 {
		return fmt.Errorf("aggregator.periodic.every_ms must be >= 0")
	}
	if cfg.Periodic.ResetBuckets && cfg.Periodic.Every == 0 {
		return fmt.Errorf("aggregator.periodic.reset_buckets requires aggregator.periodic.every_ms > 0")
	}
	if cfg.Window.IdleFlushAfter == 0 && cfg.Window.MaxFlushAfter == 0 && cfg.Periodic.Every == 0 {
		return fmt.Errorf("aggregator requires at least one export trigger: window.idle_flush_after_ms, window.max_flush_after_ms, or periodic.every_ms")
	}
	return nil
}
