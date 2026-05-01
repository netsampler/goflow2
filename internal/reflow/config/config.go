package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	DropMessage          bool                `yaml:"drop_message"`
	DropPayload          bool                `yaml:"drop_payload"`
	DisablePacketMapping bool                `yaml:"disable_packet_mapping"`
	TruncatePacketBytes  int                 `yaml:"truncate_packet_bytes"`
	PacketDecoder        PacketDecoderConfig `yaml:"packet_decoder"`
}

type PacketDecoderConfig struct {
	DecodeBeyondL4 *bool                     `yaml:"decode_beyond_l4"`
	Encapsulations PacketEncapsulationConfig `yaml:"encapsulations"`
}

type PacketEncapsulationConfig struct {
	GRE    ToggleEncapsulationConfig `yaml:"gre"`
	IPIP   ToggleEncapsulationConfig `yaml:"ipip"`
	IP6IP  ToggleEncapsulationConfig `yaml:"ip6ip"`
	VXLAN  PortEncapsulationConfig   `yaml:"vxlan"`
	Geneve PortEncapsulationConfig   `yaml:"geneve"`
	L2TP   PortEncapsulationConfig   `yaml:"l2tp"`
	GTPU   PortEncapsulationConfig   `yaml:"gtpu"`
	PPPoE  ToggleEncapsulationConfig `yaml:"pppoe"`
}

type PortEncapsulationConfig struct {
	Enabled *bool    `yaml:"enabled"`
	Ports   []uint32 `yaml:"ports"`
}

type ToggleEncapsulationConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type AggregatorConfig struct {
	Stream string `yaml:"stream"`
	// Passthrough is derived from config. When no stateful rollup is required,
	// matching events are forwarded immediately after schema registration.
	Passthrough bool `yaml:"-"`
	// Window controls bucket closure based on activity and age.
	Window AggregatorWindowConfig `yaml:"window"`
	// Periodic controls snapshot-style exports of current bucket state.
	Periodic AggregatorPeriodicConfig `yaml:"periodic"`
	// Fields is the preferred ordered field/policy list. Compact entries use:
	// role:name or static:name:value. IPFIX/NetFlow field mapping
	// stays in encoder.tflow_data.
	Fields           []AggregatorField `yaml:"fields"`
	FieldsConfigured bool              `yaml:"-"`
	KeyFields        []string          `yaml:"key_fields"`
	// Legacy aggregation policy lists. They remain supported, but no longer
	// receive hidden defaults.
	Sum          []string          `yaml:"sum"`
	First        []string          `yaml:"first"`
	Current      []string          `yaml:"current"`
	Min          []string          `yaml:"min"`
	Max          []string          `yaml:"max"`
	Match        map[string]string `yaml:"match"`
	TemplateID   uint16            `yaml:"template_id"`
	StaticFields map[string]any    `yaml:"static_fields"`

	// Deprecated compatibility knobs. They are still parsed so older configs keep
	// loading, then mapped into the explicit window/periodic sections.
	ResetInterval    int `yaml:"reset_interval_ms"`
	PeriodicInterval int `yaml:"periodic_interval_ms"`
}

type AggregatorField struct {
	Role  string `yaml:"role"`
	Name  string `yaml:"name"`
	Value any    `yaml:"value,omitempty"`

	// Path is accepted as a compatibility alias for mapping-style entries.
	Path string `yaml:"path,omitempty"`
}

func (f *AggregatorField) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		parsed, err := parseAggregatorField(value.Value)
		if err != nil {
			return err
		}
		*f = parsed
		return nil
	case yaml.MappingNode:
		type rawAggregatorField AggregatorField
		var raw rawAggregatorField
		if err := value.Decode(&raw); err != nil {
			return err
		}
		*f = AggregatorField(raw)
		if f.Name == "" {
			f.Name = f.Path
		}
		f.Path = ""
		return validateAggregatorField(*f)
	default:
		return fmt.Errorf("aggregator field must be a string or mapping")
	}
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
	Pcap                  PcapConfig      `yaml:"pcap"`
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
	AgentIP                   string               `yaml:"agent_ip"`
	CounterFormat             string               `yaml:"counter_format"`
	UseMetadataSequenceNumber bool                 `yaml:"use_metadata_sequence_number"`
	BatchOver                 SFlowBatchOverConfig `yaml:"batch_over"`
}

type SFlowBatchOverConfig struct {
	AgentIP        *bool `yaml:"agent_ip"`
	SubAgentID     *bool `yaml:"sub_agent_id"`
	SequenceNumber *bool `yaml:"sequence_number"`
	Uptime         *bool `yaml:"uptime"`
}

type PcapConfig struct {
	PacketSource string `yaml:"packet_source"`
	LinkType     string `yaml:"link_type"`
	SnapLen      int    `yaml:"snaplen"`
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
	Framing string `yaml:"framing"`
	Mode    string `yaml:"mode"`
}

// BindFlags defines the small CLI surface used to locate config and control logging.
func BindFlags(fs *flag.FlagSet) (*FlagConfig, *bool) {
	cfg := &FlagConfig{}
	version := fs.Bool("v", false, "Print version")
	fs.StringVar(&cfg.ConfigPath, "config", "cmd/reflow/reflow.yaml", "Path to ReFlow YAML config")
	fs.StringVar(&cfg.LogLevel, "loglevel", "info", "Log level")
	fs.StringVar(&cfg.LogFormat, "logfmt", "text", "Log format (text or json)")
	return cfg, version
}

// Load reads, decodes, defaults, and validates the runtime config in one step.
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

// setDefaults applies runtime defaults after YAML decoding so the rest of the
// code can treat the config as normalized.
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
	if err := validatePacketDecoderConfig(c.Processor.Builtin.PacketDecoder); err != nil {
		return fmt.Errorf("processor.builtin.packet_decoder: %w", err)
	}
	if len(c.Aggregators) > 0 {
		for i := range c.Aggregators {
			applyAggregatorCompatibility(&c.Aggregators[i])
			if err := normalizeAggregatorConfig(&c.Aggregators[i]); err != nil {
				return fmt.Errorf("aggregators[%d]: %w", i, err)
			}
			if err := validateAggregatorConfig(c.Aggregators[i]); err != nil {
				return fmt.Errorf("aggregators[%d]: %w", i, err)
			}
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
	switch c.Encoder.Pcap.PacketSource {
	case "", "auto":
		c.Encoder.Pcap.PacketSource = "auto"
	case "header_data", "payload", "pseudo":
	default:
		return fmt.Errorf("unsupported encoder.pcap.packet_source %q", c.Encoder.Pcap.PacketSource)
	}
	switch c.Encoder.Pcap.LinkType {
	case "", "ethernet":
		c.Encoder.Pcap.LinkType = "ethernet"
	case "raw", "ipv4", "ipv6":
	default:
		return fmt.Errorf("unsupported encoder.pcap.link_type %q", c.Encoder.Pcap.LinkType)
	}
	if c.Encoder.Pcap.SnapLen < 0 {
		return fmt.Errorf("encoder.pcap.snaplen must be >= 0")
	}
	if c.Encoder.Pcap.SnapLen == 0 {
		c.Encoder.Pcap.SnapLen = 65535
	}
	switch c.Encoder.Type {
	case "json", "protobuf", "sflow", "ipfix", "netflowv9", "netflowv5", "pcap", "pcapng":
	default:
		return fmt.Errorf("unsupported encoder.type %q", c.Encoder.Type)
	}
	if c.Sink.Type == "" {
		c.Sink.Type = "stdout"
	}
	if (c.Encoder.Type == "pcap" || c.Encoder.Type == "pcapng") && (c.Sink.Type == "udp" || c.Sink.Type == "unixgram") {
		return fmt.Errorf("encoder.type=%s requires a stream sink, got sink.type=%s", c.Encoder.Type, c.Sink.Type)
	}
	switch c.Sink.Type {
	case "stdout", "file", "udp", "unixgram":
	default:
		return fmt.Errorf("unsupported sink.type %q", c.Sink.Type)
	}
	if c.Sink.Framing == "" {
		if c.Encoder.Type == "pcap" || c.Encoder.Type == "pcapng" {
			c.Sink.Framing = "none"
		} else {
			c.Sink.Framing = "line"
		}
	}
	switch c.Sink.Framing {
	case "line", "none":
	default:
		return fmt.Errorf("unsupported sink.framing %q", c.Sink.Framing)
	}
	if c.Sink.Mode == "" {
		if (c.Encoder.Type == "pcap" || c.Encoder.Type == "pcapng") && c.Sink.Type == "file" {
			c.Sink.Mode = "truncate"
		} else {
			c.Sink.Mode = "append"
		}
	}
	switch c.Sink.Mode {
	case "append", "truncate":
	default:
		return fmt.Errorf("unsupported sink.mode %q", c.Sink.Mode)
	}
	if c.Sink.Type == "file" && c.Sink.Path == "" {
		return fmt.Errorf("sink.path is required when sink.type=file")
	}
	if (c.Sink.Type == "udp" || c.Sink.Type == "unixgram") && c.Sink.Address == "" {
		return fmt.Errorf("sink.address is required when sink.type=%s", c.Sink.Type)
	}
	return nil
}

func validatePacketDecoderConfig(cfg PacketDecoderConfig) error {
	if err := validateUDPPorts("encapsulations.vxlan.ports", cfg.Encapsulations.VXLAN.Ports); err != nil {
		return err
	}
	if err := validateUDPPorts("encapsulations.geneve.ports", cfg.Encapsulations.Geneve.Ports); err != nil {
		return err
	}
	if err := validateUDPPorts("encapsulations.l2tp.ports", cfg.Encapsulations.L2TP.Ports); err != nil {
		return err
	}
	if err := validateUDPPorts("encapsulations.gtpu.ports", cfg.Encapsulations.GTPU.Ports); err != nil {
		return err
	}
	return nil
}

func validateUDPPorts(name string, ports []uint32) error {
	for _, port := range ports {
		if port > 65535 {
			return fmt.Errorf("%s contains invalid UDP port %d", name, port)
		}
	}
	return nil
}

// applySourceDefaults normalizes per-source defaults that depend on source.network.
func applySourceDefaults(src *SourceConfig) error {
	if src.Network == "" {
		src.Network = "udp"
	}
	if src.Network == "stream" {
		if src.Address == "" {
			src.Address = "-"
		}
		switch src.Type {
		case "pcap", "pcapng", "json":
		case "":
			return fmt.Errorf("source.type is required when source.network=stream")
		default:
			return fmt.Errorf("unsupported source.type %q for source.network=stream", src.Type)
		}
		return nil
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

// defaultTrue fills optional boolean pointers with true so config can distinguish
// "unset" from an explicit false.
func defaultTrue(dst **bool) {
	if *dst != nil {
		return
	}
	v := true
	*dst = &v
}

// loadFlowDataCatalog resolves the IPFIX field catalog relative to the config
// file and merges file-backed definitions with inline overrides.
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

// mergeIPFIXFields applies later catalogs over earlier ones.
func mergeIPFIXFields(sources ...map[string]IPFIXFieldDefinition) map[string]IPFIXFieldDefinition {
	merged := make(map[string]IPFIXFieldDefinition)
	for _, source := range sources {
		for key, def := range source {
			merged[key] = def
		}
	}
	return merged
}

// normalizeAggregatorConfig applies defaults that do not change aggregation
// semantics and translates legacy field lists into the preferred field DSL.
func normalizeAggregatorConfig(cfg *AggregatorConfig) error {
	if cfg.Stream == "" {
		cfg.Stream = "flow_data"
	}

	cfg.FieldsConfigured = len(cfg.Fields) > 0
	if !cfg.FieldsConfigured {
		for _, field := range cfg.KeyFields {
			cfg.Fields = append(cfg.Fields, AggregatorField{Role: "key", Name: field})
		}
		for _, field := range cfg.Sum {
			cfg.Fields = append(cfg.Fields, AggregatorField{Role: "sum", Name: field})
		}
		for _, field := range cfg.First {
			cfg.Fields = append(cfg.Fields, AggregatorField{Role: "first", Name: field})
		}
		for _, field := range cfg.Current {
			cfg.Fields = append(cfg.Fields, AggregatorField{Role: "current", Name: field})
		}
		for _, field := range cfg.Min {
			cfg.Fields = append(cfg.Fields, AggregatorField{Role: "min", Name: field})
		}
		for _, field := range cfg.Max {
			cfg.Fields = append(cfg.Fields, AggregatorField{Role: "max", Name: field})
		}
		staticFields := make([]string, 0, len(cfg.StaticFields))
		for field := range cfg.StaticFields {
			staticFields = append(staticFields, field)
		}
		sort.Strings(staticFields)
		for _, field := range staticFields {
			cfg.Fields = append(cfg.Fields, AggregatorField{Role: "static", Name: field, Value: cfg.StaticFields[field]})
		}
	} else {
		cfg.KeyFields = nil
		cfg.Sum = nil
		cfg.First = nil
		cfg.Current = nil
		cfg.Min = nil
		cfg.Max = nil
		cfg.StaticFields = nil
		for _, field := range cfg.Fields {
			if err := validateAggregatorField(field); err != nil {
				return err
			}
			switch field.Role {
			case "key":
				cfg.KeyFields = append(cfg.KeyFields, field.Name)
			case "sum":
				cfg.Sum = append(cfg.Sum, field.Name)
			case "first":
				cfg.First = append(cfg.First, field.Name)
			case "current":
				cfg.Current = append(cfg.Current, field.Name)
			case "min":
				cfg.Min = append(cfg.Min, field.Name)
			case "max":
				cfg.Max = append(cfg.Max, field.Name)
			case "static":
				if cfg.StaticFields == nil {
					cfg.StaticFields = make(map[string]any)
				}
				cfg.StaticFields[field.Name] = field.Value
			}
		}
	}

	cfg.Passthrough = !aggregatorNeedsState(cfg)
	return nil
}

// applyAggregatorCompatibility maps legacy knobs into the explicit window and
// periodic sections used by the current runtime.
func applyAggregatorCompatibility(cfg *AggregatorConfig) {
	if cfg.Window.IdleFlushAfter == 0 && cfg.ResetInterval > 0 {
		cfg.Window.IdleFlushAfter = cfg.ResetInterval
	}
	if cfg.Periodic.Every == 0 && cfg.PeriodicInterval > 0 {
		cfg.Periodic.Every = cfg.PeriodicInterval
	}
}

// validateAggregatorConfig rejects timer combinations that would never emit data
// or would make periodic reset semantics ambiguous.
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
	if cfg.Passthrough {
		return nil
	}
	if cfg.Periodic.ResetBuckets && cfg.Periodic.Every == 0 {
		return fmt.Errorf("aggregator.periodic.reset_buckets requires aggregator.periodic.every_ms > 0")
	}
	if cfg.Window.IdleFlushAfter == 0 && cfg.Window.MaxFlushAfter == 0 && cfg.Periodic.Every == 0 {
		return fmt.Errorf("aggregator requires at least one export trigger: window.idle_flush_after_ms, window.max_flush_after_ms, or periodic.every_ms")
	}
	return nil
}

func parseAggregatorField(raw string) (AggregatorField, error) {
	if raw == "" {
		return AggregatorField{}, fmt.Errorf("aggregator field entry cannot be empty")
	}
	if strings.HasPrefix(raw, "static:") {
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) != 3 || parts[1] == "" {
			return AggregatorField{}, fmt.Errorf("invalid static field entry %q", raw)
		}
		field := AggregatorField{Role: "static", Name: parts[1], Value: parts[2]}
		return field, validateAggregatorField(field)
	}

	parts := strings.Split(raw, ":")
	if len(parts) != 2 {
		return AggregatorField{}, fmt.Errorf("invalid aggregator field entry %q", raw)
	}
	field := AggregatorField{
		Role: parts[0],
		Name: parts[1],
	}
	return field, validateAggregatorField(field)
}

func validateAggregatorField(field AggregatorField) error {
	switch field.Role {
	case "key", "sum", "first", "current", "min", "max", "static":
	default:
		return fmt.Errorf("unsupported aggregator field role %q", field.Role)
	}
	if field.Name == "" {
		return fmt.Errorf("aggregator field name is required for role %q", field.Role)
	}
	return nil
}

func aggregatorNeedsState(cfg *AggregatorConfig) bool {
	hasCurrent := false
	for _, field := range cfg.Fields {
		switch field.Role {
		case "sum", "first", "min", "max":
			return true
		case "current":
			hasCurrent = true
		}
	}
	return hasCurrent && aggregatorHasExportTrigger(*cfg)
}

func aggregatorHasExportTrigger(cfg AggregatorConfig) bool {
	return cfg.Window.IdleFlushAfter > 0 || cfg.Window.MaxFlushAfter > 0 || cfg.Periodic.Every > 0
}
