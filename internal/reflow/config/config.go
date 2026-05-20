package config

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed default-fields.yaml
var defaultFlowFields []byte

type FlagConfig struct {
	ConfigPath        string
	LogLevel          string
	LogFormat         string
	Inputs            []string
	Output            string
	OutputSet         bool
	Aggregate         bool
	AggIdleFlushAfter *int
	AggMaxFlushAfter  *int
	AggIdleEraseAfter *int
	AggPeriodicEvery  *int
	AggResetBuckets   *bool
	GenConf           bool
	ListOptions       bool
}

type Config struct {
	LogLevel        string                `yaml:"-"`
	LogFormat       string                `yaml:"-"`
	Sources         []SourceConfig        `yaml:"sources"`
	Processor       ProcessorConfig       `yaml:"processor"`
	Aggregators     []AggregatorConfig    `yaml:"aggregators"`
	TemplatedFields TemplatedFieldsConfig `yaml:"templated_fields,omitempty"`
	Encoder         EncoderConfig         `yaml:"encoder"`
	Sink            SinkConfig            `yaml:"sink"`
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
	EBPF         EBPFConfig `yaml:"ebpf,omitempty"`
}

type EBPFConfig struct {
	SKBMetadata   *bool  `yaml:"skb_metadata,omitempty"`
	Conntrack     *bool  `yaml:"conntrack,omitempty"`
	ConntrackPath string `yaml:"conntrack_path,omitempty"`
}

func (c EBPFConfig) IsZero() bool {
	return c.SKBMetadata == nil && c.Conntrack == nil && c.ConntrackPath == ""
}

func (c EBPFConfig) SKBMetadataEnabled() bool {
	return c.SKBMetadata == nil || *c.SKBMetadata
}

func (c EBPFConfig) ConntrackEnabled() bool {
	return c.Conntrack == nil || *c.Conntrack
}

type ProcessorConfig struct {
	Type    string                 `yaml:"type"`
	Workers WorkerCount            `yaml:"workers"`
	Builtin BuiltinProcessorConfig `yaml:"builtin"`
}

type WorkerCount int

const AutoWorkers WorkerCount = -1

func (w *WorkerCount) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("workers must be an integer or \"auto\"")
	}
	if strings.EqualFold(value.Value, "auto") {
		*w = AutoWorkers
		return nil
	}
	var count int
	if err := value.Decode(&count); err != nil {
		return fmt.Errorf("workers must be an integer or \"auto\"")
	}
	*w = WorkerCount(count)
	return nil
}

func (w WorkerCount) MarshalYAML() (any, error) {
	if w == AutoWorkers {
		return "auto", nil
	}
	return int(w), nil
}

func ResolveProcessorWorkers(workers WorkerCount) int {
	if workers == AutoWorkers {
		return autoWorkerCount()
	}
	if workers <= 0 {
		return 1
	}
	return int(workers)
}

func ResolveEncoderWorkers(workers WorkerCount, encoderType string) int {
	if workers == AutoWorkers {
		if orderedEncoderType(encoderType) {
			return 1
		}
		return autoWorkerCount()
	}
	if workers <= 0 {
		return 1
	}
	return int(workers)
}

func autoWorkerCount() int {
	workers := runtime.NumCPU()
	if workers < 1 {
		return 1
	}
	return workers
}

func orderedEncoderType(encoderType string) bool {
	switch encoderType {
	case "sflow", "ipfix", "netflowv9", "netflowv5":
		return true
	default:
		return false
	}
}

type BuiltinProcessorConfig struct {
	DropMessage          bool                    `yaml:"drop_message"`
	DropPayload          bool                    `yaml:"drop_payload"`
	DisablePacketMapping bool                    `yaml:"disable_packet_mapping"`
	TruncatePacketBytes  int                     `yaml:"truncate_packet_bytes"`
	PacketDecoder        PacketDecoderConfig     `yaml:"packet_decoder"`
	AggregationHelpers   AggregationHelperConfig `yaml:"aggregation_helpers"`
}

type AggregationHelperConfig struct {
	MPLSLabels int `yaml:"mpls_labels"`
	IPLayers   int `yaml:"ip_layers"`
}

type PacketDecoderConfig struct {
	DecodeBeyondL4 *bool                     `yaml:"decode_beyond_l4"`
	Encapsulations PacketEncapsulationConfig `yaml:"encapsulations"`
}

type PacketEncapsulationConfig struct {
	GRE    ToggleEncapsulationConfig `yaml:"gre"`
	IPIP   ToggleEncapsulationConfig `yaml:"ipip"`
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

func (cfg AggregatorConfig) MarshalYAML() (any, error) {
	if len(cfg.Fields) == 0 {
		type rawAggregatorConfig AggregatorConfig
		return rawAggregatorConfig(cfg), nil
	}
	return struct {
		Stream     string                   `yaml:"stream,omitempty"`
		Window     AggregatorWindowConfig   `yaml:"window,omitempty"`
		Periodic   AggregatorPeriodicConfig `yaml:"periodic,omitempty"`
		Fields     []AggregatorField        `yaml:"fields"`
		Match      map[string]string        `yaml:"match,omitempty"`
		TemplateID uint16                   `yaml:"template_id,omitempty"`
	}{
		Stream:     cfg.Stream,
		Window:     cfg.Window,
		Periodic:   cfg.Periodic,
		Fields:     cfg.Fields,
		Match:      cfg.Match,
		TemplateID: cfg.TemplateID,
	}, nil
}

type AggregatorField struct {
	Role  string `yaml:"role"`
	Name  string `yaml:"name"`
	Value any    `yaml:"value,omitempty"`

	// Path is accepted as a compatibility alias for mapping-style entries.
	Path string `yaml:"path,omitempty"`
}

func (f AggregatorField) MarshalYAML() (any, error) {
	if f.Name == "" {
		f.Name = f.Path
	}
	if err := validateAggregatorField(f); err != nil {
		return nil, err
	}
	if f.Role == "static" {
		if value, ok := f.Value.(string); ok {
			return "static:" + f.Name + ":" + value, nil
		}
		return struct {
			Role  string `yaml:"role"`
			Name  string `yaml:"name"`
			Value any    `yaml:"value,omitempty"`
		}{
			Role:  f.Role,
			Name:  f.Name,
			Value: f.Value,
		}, nil
	}
	return f.Role + ":" + f.Name, nil
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
	Type             string              `yaml:"type"`
	Workers          WorkerCount         `yaml:"workers"`
	MaxDatagramBytes int                 `yaml:"max_datagram_bytes"`
	AllowTruncate    *bool               `yaml:"allow_truncate"`
	Batch            BatchConfig         `yaml:"batch"`
	TemplatedFlow    TemplatedFlowConfig `yaml:"templated_flow"`
	JSON             JSONConfig          `yaml:"json"`
	Protobuf         ProtobufConfig      `yaml:"protobuf"`
	SFlow            SFlowConfig         `yaml:"sflow"`
	Pcap             PcapConfig          `yaml:"pcap"`
}

type TemplatedFlowConfig struct {
	TemplateBaseID        uint16                  `yaml:"template_base_id"`
	OptionsTemplateBaseID uint16                  `yaml:"options_template_base_id"`
	ObservationDomainID   uint32                  `yaml:"observation_domain_id"`
	TemplateRefresh       int                     `yaml:"template_refresh_ms"`
	OptionsRefresh        int                     `yaml:"options_refresh_ms"`
	Data                  TemplatedFlowDataConfig `yaml:"data"`
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
	Enabled       *bool `yaml:"enabled"`
	MaxRecords    int   `yaml:"max_records"`
	MaxBytes      int   `yaml:"max_bytes"`
	FlushInterval int   `yaml:"flush_interval_ms"`
}

func (c BatchConfig) IsEnabled() bool {
	return c.Enabled != nil && *c.Enabled
}

type SFlowConfig struct {
	AgentIP                   string               `yaml:"agent_ip"`
	CounterFormat             string               `yaml:"counter_format"`
	UseMetadataSequenceNumber bool                 `yaml:"use_metadata_sequence_number"`
	MaxHeaderBytes            int                  `yaml:"max_header_bytes"`
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

type TemplatedFlowDataConfig struct {
	Select     []string                        `yaml:"fields"`
	FieldsPath string                          `yaml:"fields_path"`
	Catalog    map[string]IPFIXFieldDefinition `yaml:"-"`
	Overrides  map[string]IPFIXFieldDefinition `yaml:"overrides"`
}

type TemplatedFieldsConfig struct {
	FieldsPath string                          `yaml:"fields_path"`
	Catalog    map[string]IPFIXFieldDefinition `yaml:"-"`
	Overrides  map[string]IPFIXFieldDefinition `yaml:"overrides"`
}

func (c TemplatedFieldsConfig) IsZero() bool {
	return c.FieldsPath == "" && len(c.Overrides) == 0
}

type IPFIXFieldDefinition struct {
	Name             string `yaml:"name"`
	ID               uint16 `yaml:"id"`
	PEN              uint32 `yaml:"pen"`
	Length           uint16 `yaml:"length"`
	Type             string `yaml:"type"`
	Format           string `yaml:"format"`
	EnterpriseScoped bool   `yaml:"enterprise_scoped"`
}

func (d *IPFIXFieldDefinition) UnmarshalYAML(value *yaml.Node) error {
	type rawDefinition IPFIXFieldDefinition
	if value.Kind != yaml.ScalarNode {
		var raw rawDefinition
		if err := value.Decode(&raw); err != nil {
			return err
		}
		*d = IPFIXFieldDefinition(raw)
		return nil
	}

	var compact string
	if err := value.Decode(&compact); err != nil {
		return err
	}
	def, err := parseCompactFieldDefinition(compact)
	if err != nil {
		return err
	}
	*d = def
	return nil
}

func parseCompactFieldDefinition(raw string) (IPFIXFieldDefinition, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return IPFIXFieldDefinition{}, fmt.Errorf("empty field definition")
	}

	var penInfo string
	if open := strings.LastIndex(raw, "["); open >= 0 {
		if !strings.HasSuffix(raw, "]") {
			return IPFIXFieldDefinition{}, fmt.Errorf("invalid field definition %q: missing closing bracket", raw)
		}
		penInfo = strings.TrimSpace(raw[open+1 : len(raw)-1])
		raw = strings.TrimSpace(raw[:open])
	}

	parts := strings.Split(raw, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return IPFIXFieldDefinition{}, fmt.Errorf("invalid field definition %q: expected id:length:type[:format]", raw)
	}
	id, err := parseUint16Part(parts[0], "id")
	if err != nil {
		return IPFIXFieldDefinition{}, err
	}
	length, err := parseUint16Part(parts[1], "length")
	if err != nil {
		return IPFIXFieldDefinition{}, err
	}
	def := IPFIXFieldDefinition{
		ID:     id,
		Length: length,
		Type:   expandFieldTypeAlias(parts[2]),
	}
	if len(parts) == 4 {
		def.Format = strings.TrimSpace(parts[3])
	}
	if penInfo != "" {
		pen, enterpriseScoped, err := parsePENInfo(penInfo)
		if err != nil {
			return IPFIXFieldDefinition{}, err
		}
		def.PEN = pen
		def.EnterpriseScoped = enterpriseScoped
	}
	return def, nil
}

func parseUint16Part(raw, name string) (uint16, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(raw), 0, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", name, raw, err)
	}
	return uint16(v), nil
}

func expandFieldTypeAlias(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "u8":
		return "unsigned8"
	case "u16":
		return "unsigned16"
	case "u32":
		return "unsigned32"
	case "u64":
		return "unsigned64"
	case "s8":
		return "signed8"
	case "s16":
		return "signed16"
	case "s32":
		return "signed32"
	case "s64":
		return "signed64"
	case "ip4", "ipv4":
		return "ipv4Address"
	case "ip6", "ipv6":
		return "ipv6Address"
	case "mac":
		return "macAddress"
	case "str":
		return "string"
	default:
		return strings.TrimSpace(raw)
	}
}

func parsePENInfo(raw string) (uint32, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	enterpriseScoped := true
	if key, val, ok := strings.Cut(raw, "="); ok {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "pen":
			raw = val
		case "enterprise", "enterprise_scoped":
			enterpriseScoped = strings.TrimSpace(val) != "false"
			return 0, enterpriseScoped, nil
		default:
			return 0, false, fmt.Errorf("invalid PEN info key %q", key)
		}
	}
	pen, err := strconv.ParseUint(strings.TrimSpace(raw), 0, 32)
	if err != nil {
		return 0, false, fmt.Errorf("invalid PEN info %q: %w", raw, err)
	}
	return uint32(pen), enterpriseScoped, nil
}

type SinkConfig struct {
	Type    string `yaml:"type"`
	Path    string `yaml:"path"`
	Address string `yaml:"address"`
	Framing string `yaml:"framing"`
	Mode    string `yaml:"mode"`
}

// BindFlags defines CLI bootstrap flags. When -config is omitted, helper flags
// generate an in-memory config and pass it through the same defaults/validation.
func BindFlags(fs *flag.FlagSet) (*FlagConfig, *bool) {
	cfg := &FlagConfig{}
	version := fs.Bool("v", false, "Print version")
	fs.StringVar(&cfg.ConfigPath, "config", "", "Path to ReFlow YAML config")
	fs.StringVar(&cfg.LogLevel, "loglevel", "info", "Log level")
	fs.StringVar(&cfg.LogFormat, "logfmt", "text", "Log format (text or json)")
	fs.Var((*inputFlags)(&cfg.Inputs), "input", "Input helper spec network:target:type (repeatable)")
	out := outputFlag{cfg: cfg}
	fs.Var(out, "output", "Output helper spec encoder:sink[:target]")
	fs.Var(out, "o", "Output helper spec encoder:sink[:target]")
	fs.Var(aggregateFlag{cfg: cfg}, "agg", "Generate packet aggregation config")
	fs.BoolVar(&cfg.GenConf, "genconf", false, "Print generated config and exit")
	fs.BoolVar(&cfg.ListOptions, "list-options", false, "List helper -input/-output options and exit")
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
	if c.Processor.Workers == 0 {
		c.Processor.Workers = 1
	} else if c.Processor.Workers < 0 && c.Processor.Workers != AutoWorkers {
		return fmt.Errorf("processor.workers must be >= 1 or \"auto\"")
	}
	if c.Processor.Builtin.TruncatePacketBytes < 0 {
		return fmt.Errorf("processor.builtin.truncate_packet_bytes must be >= 0")
	}
	if c.Processor.Builtin.AggregationHelpers.MPLSLabels < 0 {
		return fmt.Errorf("processor.builtin.aggregation_helpers.mpls_labels must be >= 0")
	}
	if c.Processor.Builtin.AggregationHelpers.IPLayers < 0 {
		return fmt.Errorf("processor.builtin.aggregation_helpers.ip_layers must be >= 0")
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
	if c.Encoder.Workers == 0 {
		c.Encoder.Workers = 1
	} else if c.Encoder.Workers < 0 && c.Encoder.Workers != AutoWorkers {
		return fmt.Errorf("encoder.workers must be >= 1 or \"auto\"")
	}
	if c.Encoder.AllowTruncate == nil {
		v := c.Encoder.Type == "sflow"
		c.Encoder.AllowTruncate = &v
	}
	if c.Encoder.Batch.IsEnabled() {
		if c.Encoder.Batch.MaxRecords == 0 {
			c.Encoder.Batch.MaxRecords = 8
		}
		if c.Encoder.Batch.MaxBytes == 0 {
			c.Encoder.Batch.MaxBytes = 1200
		}
		if c.Encoder.Batch.FlushInterval == 0 {
			c.Encoder.Batch.FlushInterval = 1000
		}
	}
	if c.Encoder.MaxDatagramBytes <= 0 {
		c.Encoder.MaxDatagramBytes = 1400
		if c.Encoder.Batch.IsEnabled() && c.Encoder.Batch.MaxBytes > 0 {
			c.Encoder.MaxDatagramBytes = c.Encoder.Batch.MaxBytes
		}
	}
	if (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") && c.Encoder.TemplatedFlow.TemplateBaseID == 0 {
		c.Encoder.TemplatedFlow.TemplateBaseID = 256
	}
	if (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") && c.Encoder.TemplatedFlow.OptionsTemplateBaseID == 0 {
		c.Encoder.TemplatedFlow.OptionsTemplateBaseID = 1024
	}
	if c.Encoder.TemplatedFlow.TemplateRefresh < 0 {
		return fmt.Errorf("encoder.templated_flow.template_refresh_ms must be >= 0")
	}
	if c.Encoder.TemplatedFlow.OptionsRefresh < 0 {
		return fmt.Errorf("encoder.templated_flow.options_refresh_ms must be >= 0")
	}
	if (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") && c.Encoder.TemplatedFlow.TemplateRefresh == 0 {
		c.Encoder.TemplatedFlow.TemplateRefresh = 60000
	}
	if (c.Encoder.Type == "ipfix" || c.Encoder.Type == "netflowv9") && c.Encoder.TemplatedFlow.OptionsRefresh == 0 {
		c.Encoder.TemplatedFlow.OptionsRefresh = 30000
	}
	defaultTrue(&c.Encoder.SFlow.BatchOver.AgentIP)
	defaultTrue(&c.Encoder.SFlow.BatchOver.SubAgentID)
	defaultTrue(&c.Encoder.SFlow.BatchOver.SequenceNumber)
	defaultTrue(&c.Encoder.SFlow.BatchOver.Uptime)
	if c.Encoder.SFlow.MaxHeaderBytes < 0 {
		return fmt.Errorf("encoder.sflow.max_header_bytes must be >= 0")
	}
	if c.Encoder.Type == "sflow" && c.Encoder.SFlow.MaxHeaderBytes == 0 {
		c.Encoder.SFlow.MaxHeaderBytes = 128
	}
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
	if src.Network != "pcap_live" && src.Network != "ebpf" && src.Address == "" {
		src.Address = ":18080"
	}
	if src.Network == "pcap_live" || src.Network == "ebpf" {
		if src.Interface == "" {
			return fmt.Errorf("source.interface is required when source.network=%s", src.Network)
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
		if src.Network == "ebpf" && src.Type != "bytes" {
			return fmt.Errorf("source.type must be bytes when source.network=ebpf")
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

func defaultFalse(dst **bool) {
	if *dst != nil {
		return
	}
	v := false
	*dst = &v
}

// loadFlowDataCatalog resolves the shared templated flow field catalog. Empty
// fields_path uses the embedded default catalog; explicit paths are resolved
// relative to config. Legacy encoder.templated_flow.data catalog settings keep
// their previous behavior when the top-level shared catalog is not configured.
func (c *Config) loadFlowDataCatalog(configPath string) error {
	sharedConfigured := c.TemplatedFields.FieldsPath != "" || len(c.TemplatedFields.Overrides) > 0 || len(c.TemplatedFields.Catalog) > 0
	if sharedConfigured {
		fields, err := loadIPFIXFieldCatalog(configPath, &c.TemplatedFields.FieldsPath, "templated_fields")
		if err != nil {
			return err
		}
		shared := mergeIPFIXFields(fields, c.TemplatedFields.Catalog, c.TemplatedFields.Overrides)
		if err := validateTemplatedDecodeCatalog(shared); err != nil {
			return fmt.Errorf("templated_fields: %w", err)
		}
		c.TemplatedFields.Catalog = shared
		c.Encoder.TemplatedFlow.Data.Catalog = mergeIPFIXFields(shared, c.Encoder.TemplatedFlow.Data.Catalog, c.Encoder.TemplatedFlow.Data.Overrides)
		return nil
	}

	fields, err := loadIPFIXFieldCatalog(configPath, &c.Encoder.TemplatedFlow.Data.FieldsPath, "encoder.templated_flow.data")
	if err != nil {
		return err
	}
	legacy := mergeIPFIXFields(fields, c.Encoder.TemplatedFlow.Data.Catalog, c.Encoder.TemplatedFlow.Data.Overrides)
	if err := validateTemplatedDecodeCatalog(legacy); err != nil {
		return fmt.Errorf("encoder.templated_flow.data: %w", err)
	}
	c.TemplatedFields.Catalog = legacy
	c.Encoder.TemplatedFlow.Data.Catalog = legacy
	return nil
}

func loadIPFIXFieldCatalog(configPath string, fieldsPath *string, label string) (map[string]IPFIXFieldDefinition, error) {
	type ipfixCatalog struct {
		Fields map[string]IPFIXFieldDefinition `yaml:"fields"`
	}

	raw := defaultFlowFields
	source := "embedded default flow fields"
	if fieldsPath != nil && *fieldsPath != "" {
		if !filepath.IsAbs(*fieldsPath) {
			*fieldsPath = filepath.Join(filepath.Dir(configPath), *fieldsPath)
		}
		source = *fieldsPath
		var err error
		raw, err = os.ReadFile(*fieldsPath)
		if err != nil {
			return nil, fmt.Errorf("load %s fields %s: %w", label, source, err)
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			raw = defaultFlowFields
			source = "embedded default flow fields"
		}
	}

	catalog := ipfixCatalog{}
	if err := yaml.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("decode %s fields %s: %w", label, source, err)
	}
	return catalog.Fields, nil
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

type decodeIPFIXKey struct {
	id  uint16
	pen uint32
}

func validateTemplatedDecodeCatalog(catalog map[string]IPFIXFieldDefinition) error {
	ipfix := make(map[decodeIPFIXKey]string)
	netflowV9 := make(map[uint16]string)
	for name, def := range catalog {
		for _, key := range ipfixDecodeKeys(name, def) {
			if existing, ok := ipfix[key]; ok && existing != name {
				return fmt.Errorf("duplicate IPFIX decode mapping id=%d pen=%d for %q and %q", key.id, key.pen, existing, name)
			}
			ipfix[key] = name
		}
		for _, id := range netflowV9DecodeIDs(name, def) {
			if existing, ok := netflowV9[id]; ok && existing != name {
				return fmt.Errorf("duplicate NetFlow v9 decode mapping id=%d for %q and %q", id, existing, name)
			}
			netflowV9[id] = name
		}
	}
	return nil
}

func ipfixDecodeKeys(name string, def IPFIXFieldDefinition) []decodeIPFIXKey {
	if def.ID == 0 {
		return nil
	}
	pen := uint32(0)
	if def.EnterpriseScoped || def.PEN != 0 {
		pen = def.PEN
	}
	keys := []decodeIPFIXKey{{id: def.ID, pen: pen}}
	switch name {
	case "src_addr":
		keys = append(keys, decodeIPFIXKey{id: 27})
	case "dst_addr":
		keys = append(keys, decodeIPFIXKey{id: 28})
	}
	return keys
}

func netflowV9DecodeIDs(name string, def IPFIXFieldDefinition) []uint16 {
	ids := make([]uint16, 0, 2)
	if def.ID != 0 && def.PEN == 0 && !def.EnterpriseScoped {
		ids = append(ids, def.ID)
	}
	switch name {
	case "src_addr":
		ids = append(ids, 27)
	case "dst_addr":
		ids = append(ids, 28)
	case "start_time_unix":
		ids = append(ids, 21)
	case "end_time_unix":
		ids = append(ids, 22)
	}
	return ids
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
