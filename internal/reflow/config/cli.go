package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const generatedConfigPath = "cmd/reflow/reflow.yaml"

var (
	inputHelperOptions = []string{
		"udp:<listen-address>:flow",
		"udp:<listen-address>:bytes",
		"udp:<listen-address>:json",
		"unixgram:<socket-path>:flow",
		"unixgram:<socket-path>:bytes",
		"unixgram:<socket-path>:json",
		"socket:<socket-path>:flow",
		"socket:<socket-path>:bytes",
		"socket:<socket-path>:json",
		"stream:<path-or-stdin>:pcap",
		"stream:<path-or-stdin>:pcapng",
		"stream:<path-or-stdin>:json",
		"ebpf:<interface>:bytes[?snaplen=<bytes>&sample_every=<n>&sample_offset=<n>&skb_metadata=<bool>&conntrack=<bool>]",
		"pcap_live:<interface>:bytes[?snaplen=<bytes>&sample_every=<n>&sample_offset=<n>]",
	}
	outputEncoderOptions = []string{
		"json",
		"protobuf",
		"sflow",
		"ipfix",
		"netflowv9",
		"netflowv5",
		"pcap",
		"pcapng",
	}
	outputSinkOptions = []string{
		"stdout",
		"file:<path>",
		"udp:<address>",
		"unixgram:<socket-path>",
		"socket:<socket-path>",
	}
	outputHelperOptions = []string{
		"sflow:*[?allow_truncate=<bool>&max_header_bytes=<bytes>]",
		"sflow|ipfix:*[?batch=<bool>&batch_max_records=<n>&batch_max_bytes=<bytes>&batch_flush_interval_ms=<ms>]",
	}
	inputHelperExamples = []string{
		"udp::6343:flow",
		"udp:127.0.0.1:2055:flow",
		"stream:-:json",
		"ebpf:eth0:bytes",
		"'pcap_live:en0:bytes?snaplen=262144&sample_every=10'",
	}
	outputHelperExamples = []string{
		"json:stdout",
		"ipfix:udp:127.0.0.1:4739",
		"protobuf:file:/tmp/reflow.pb",
		"pcap:stdout",
		"'sflow:udp:127.0.0.1:6343?allow_truncate=true&max_header_bytes=128'",
		"'ipfix:udp:127.0.0.1:4739?batch=true&batch_max_records=32&batch_max_bytes=4096&batch_flush_interval_ms=250'",
	}
	aggregateHelperOptions = []string{
		"-agg",
		"-agg=payload",
		"-agg=passthrough,payload",
		"-agg=idle_flush_after_ms=<ms>,periodic_every_ms=<ms>",
		"-agg=max_flush_after_ms=<ms>,idle_erase_after_ms=<ms>,reset_buckets=<bool>",
	}
	aggregateHelperExamples = []string{
		"-agg=payload",
		"-agg=passthrough,payload",
		"-agg=idle_flush_after_ms=5000,periodic_every_ms=30000",
		"-agg=periodic_every_ms=10000,reset_buckets=true",
	}
)

type inputFlags []string

func (f *inputFlags) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func HelperOptionsText() string {
	var b strings.Builder
	b.WriteString("Input helper specs (-input network:target:type):\n")
	for _, option := range inputHelperOptions {
		fmt.Fprintf(&b, "  %s\n", option)
	}
	b.WriteString("\nInput examples:\n")
	for _, example := range inputHelperExamples {
		fmt.Fprintf(&b, "  -input %s\n", example)
	}
	b.WriteString("\nOutput helper specs (-output encoder:sink[:target]):\n")
	b.WriteString("  encoders: ")
	b.WriteString(strings.Join(outputEncoderOptions, ", "))
	b.WriteString("\n  sinks:\n")
	for _, option := range outputSinkOptions {
		fmt.Fprintf(&b, "    %s\n", option)
	}
	b.WriteString("  parameters:\n")
	for _, option := range outputHelperOptions {
		fmt.Fprintf(&b, "    %s\n", option)
	}
	b.WriteString("\nOutput examples:\n")
	for _, example := range outputHelperExamples {
		fmt.Fprintf(&b, "  -output %s\n", example)
	}
	b.WriteString("\nAggregation helper specs:\n")
	for _, option := range aggregateHelperOptions {
		fmt.Fprintf(&b, "  %s\n", option)
	}
	b.WriteString("\nAggregation examples:\n")
	for _, example := range aggregateHelperExamples {
		fmt.Fprintf(&b, "  %s\n", example)
	}
	return b.String()
}

func (f *inputFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

type outputFlag struct {
	cfg *FlagConfig
}

func (f outputFlag) String() string {
	if f.cfg == nil {
		return ""
	}
	return f.cfg.Output
}

func (f outputFlag) Set(value string) error {
	if f.cfg.OutputSet {
		return fmt.Errorf("only one -output/-o value is supported")
	}
	f.cfg.Output = value
	f.cfg.OutputSet = true
	return nil
}

type aggregateFlag struct {
	cfg *FlagConfig
}

func (f aggregateFlag) String() string {
	if f.cfg == nil || !f.cfg.Aggregate {
		return "false"
	}
	var parts []string
	if f.cfg.AggIdleFlushAfter != nil {
		parts = append(parts, fmt.Sprintf("idle_flush_after_ms=%d", *f.cfg.AggIdleFlushAfter))
	}
	if f.cfg.AggMaxFlushAfter != nil {
		parts = append(parts, fmt.Sprintf("max_flush_after_ms=%d", *f.cfg.AggMaxFlushAfter))
	}
	if f.cfg.AggIdleEraseAfter != nil {
		parts = append(parts, fmt.Sprintf("idle_erase_after_ms=%d", *f.cfg.AggIdleEraseAfter))
	}
	if f.cfg.AggPeriodicEvery != nil {
		parts = append(parts, fmt.Sprintf("periodic_every_ms=%d", *f.cfg.AggPeriodicEvery))
	}
	if f.cfg.AggResetBuckets != nil {
		parts = append(parts, fmt.Sprintf("reset_buckets=%t", *f.cfg.AggResetBuckets))
	}
	parts = append(parts, f.cfg.AggPresets...)
	if len(parts) == 0 {
		return "true"
	}
	return strings.Join(parts, ",")
}

func (f aggregateFlag) Set(value string) error {
	if f.cfg == nil {
		return fmt.Errorf("missing aggregate flag config")
	}
	value = strings.TrimSpace(value)
	switch value {
	case "", "true":
		f.cfg.Aggregate = true
		return nil
	case "false":
		f.cfg.Aggregate = false
		return nil
	}
	f.cfg.Aggregate = true
	return parseAggregateParams(f.cfg, value)
}

func (aggregateFlag) IsBoolFlag() bool { return true }

// LoadFromFlags resolves either explicit YAML mode or generated-helper mode.
func LoadFromFlags(flags *FlagConfig) (*Config, bool, error) {
	if flags == nil {
		flags = &FlagConfig{}
	}
	if flags.ConfigPath != "" {
		if flags.usesGeneratedHelpersWithConfig() {
			return nil, false, fmt.Errorf("-config cannot be combined with -input, -output/-o, or -genconf")
		}
		cfg, err := Load(flags.ConfigPath)
		if err != nil {
			return nil, false, err
		}
		if err := cfg.ApplyAggregationFlags(flags); err != nil {
			return nil, false, err
		}
		return cfg, false, nil
	}

	cfg, err := flags.generatedConfig()
	if err != nil {
		return nil, true, err
	}
	if err := cfg.setDefaults(generatedConfigPath); err != nil {
		return nil, true, err
	}
	if flags.GenConf {
		cfg.materializeGeneratedYAMLDefaults()
	}
	return cfg, true, nil
}

func (c *FlagConfig) usesGeneratedHelpers() bool {
	return len(c.Inputs) > 0 || c.OutputSet || c.Aggregate || c.GenConf
}

func (c *FlagConfig) usesGeneratedHelpersWithConfig() bool {
	return len(c.Inputs) > 0 || c.OutputSet || c.GenConf
}

func (c *FlagConfig) generatedConfig() (*Config, error) {
	inputs := c.Inputs
	if len(inputs) == 0 {
		inputs = []string{
			"udp::6343:flow",
			"udp::2055:flow",
		}
	}

	sources := make([]SourceConfig, 0, len(inputs))
	for _, spec := range inputs {
		source, err := parseInputSpec(spec)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}

	output := c.Output
	if output == "" {
		output = "json:stdout"
	}
	encoder, sink, err := parseOutputSpec(output)
	if err != nil {
		return nil, err
	}
	cfg := &Config{
		Sources: sources,
		Processor: ProcessorConfig{
			Type:    "builtin",
			Workers: AutoWorkers,
			Builtin: BuiltinProcessorConfig{
				DropMessage: true,
				DropPayload: true,
			},
		},
		Encoder: encoder,
		Sink:    sink,
	}
	if c.Aggregate {
		cfg.Aggregators = generatedAggregators(c)
	}
	return cfg, nil
}

func (c *Config) materializeGeneratedYAMLDefaults() {
	for i := range c.Sources {
		if c.Sources[i].SnapLen == 0 {
			c.Sources[i].SnapLen = 65535
		}
		if c.Sources[i].SampleEvery == 0 {
			c.Sources[i].SampleEvery = 1
		}
		if c.Sources[i].Network == "ebpf" {
			defaultTrue(&c.Sources[i].EBPF.SKBMetadata)
			defaultTrue(&c.Sources[i].EBPF.Conntrack)
		}
	}
	defaultFalse(&c.Processor.Builtin.PacketDecoder.DecodeBeyondL4)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.GRE.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.IPIP.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.VXLAN.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.Geneve.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.L2TP.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.GTPU.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.PPPoE.Enabled)
	defaultFalse(&c.Encoder.Batch.Enabled)
	if len(c.Processor.Builtin.PacketDecoder.Encapsulations.VXLAN.Ports) == 0 {
		c.Processor.Builtin.PacketDecoder.Encapsulations.VXLAN.Ports = []uint32{4789}
	}
	if len(c.Processor.Builtin.PacketDecoder.Encapsulations.Geneve.Ports) == 0 {
		c.Processor.Builtin.PacketDecoder.Encapsulations.Geneve.Ports = []uint32{6081}
	}
	if len(c.Processor.Builtin.PacketDecoder.Encapsulations.L2TP.Ports) == 0 {
		c.Processor.Builtin.PacketDecoder.Encapsulations.L2TP.Ports = []uint32{1701}
	}
	if len(c.Processor.Builtin.PacketDecoder.Encapsulations.GTPU.Ports) == 0 {
		c.Processor.Builtin.PacketDecoder.Encapsulations.GTPU.Ports = []uint32{2152}
	}
	if c.Encoder.TemplatedFlow.TemplateBaseID == 0 {
		c.Encoder.TemplatedFlow.TemplateBaseID = 256
	}
	if c.Encoder.TemplatedFlow.OptionsTemplateBaseID == 0 {
		c.Encoder.TemplatedFlow.OptionsTemplateBaseID = 1024
	}
	if c.Encoder.TemplatedFlow.TemplateRefresh == 0 {
		c.Encoder.TemplatedFlow.TemplateRefresh = 60000
	}
	if c.Encoder.TemplatedFlow.OptionsRefresh == 0 {
		c.Encoder.TemplatedFlow.OptionsRefresh = 30000
	}
}

func parseInputSpec(spec string) (SourceConfig, error) {
	network, rest, ok := strings.Cut(spec, ":")
	if !ok || network == "" || rest == "" {
		return SourceConfig{}, fmt.Errorf("invalid -input %q: expected network:target:type", spec)
	}
	target, sourceType, ok := cutLast(rest, ":")
	if !ok || sourceType == "" {
		return SourceConfig{}, fmt.Errorf("invalid -input %q: expected network:target:type", spec)
	}
	network = normalizeSocketAlias(network)
	sourceType, rawParams, ok := strings.Cut(sourceType, "?")
	if ok && rawParams == "" {
		return SourceConfig{}, fmt.Errorf("invalid -input %q: source parameters cannot be empty", spec)
	}

	source := SourceConfig{
		Network: network,
		Address: target,
		Type:    sourceType,
	}
	if source.Type == "json" {
		source.JSON.Flavor = "reflow"
	}

	switch network {
	case "udp":
		if err := validateSocketSourceType(sourceType); err != nil {
			return SourceConfig{}, fmt.Errorf("invalid -input %q: %w", spec, err)
		}
		if err := validateUDPListenTarget(target); err != nil {
			return SourceConfig{}, fmt.Errorf("invalid -input %q: %w", spec, err)
		}
	case "unixgram":
		if err := validateSocketSourceType(sourceType); err != nil {
			return SourceConfig{}, fmt.Errorf("invalid -input %q: %w", spec, err)
		}
	case "stream":
		switch sourceType {
		case "pcap", "pcapng", "json":
		default:
			return SourceConfig{}, fmt.Errorf("invalid -input %q: stream source type must be pcap, pcapng, or json", spec)
		}
	case "pcap_live", "ebpf":
		if sourceType != "bytes" {
			return SourceConfig{}, fmt.Errorf("invalid -input %q: %s source type must be bytes", spec, network)
		}
		source.Interface = target
		source.Address = ""
	default:
		return SourceConfig{}, fmt.Errorf("invalid -input %q: unsupported network %q", spec, network)
	}
	if rawParams != "" {
		if err := applyInputParams(&source, rawParams); err != nil {
			return SourceConfig{}, fmt.Errorf("invalid -input %q: %w", spec, err)
		}
	}
	return source, nil
}

func applyInputParams(source *SourceConfig, rawParams string) error {
	if source.Network != "pcap_live" && source.Network != "ebpf" {
		return fmt.Errorf("source parameters are only supported for pcap_live and ebpf inputs")
	}
	params, err := url.ParseQuery(rawParams)
	if err != nil {
		return fmt.Errorf("parse source parameters: %w", err)
	}
	for key, values := range params {
		if len(values) != 1 {
			return fmt.Errorf("source parameter %q must be set once", key)
		}
		value := values[0]
		switch key {
		case "snaplen":
			parsed, err := parseNonNegativeInputParam(key, value)
			if err != nil {
				return err
			}
			source.SnapLen = parsed
		case "sample_every":
			parsed, err := parseNonNegativeInputParam(key, value)
			if err != nil {
				return err
			}
			source.SampleEvery = parsed
		case "sample_offset":
			parsed, err := parseNonNegativeInputParam(key, value)
			if err != nil {
				return err
			}
			source.SampleOffset = parsed
		case "skb_metadata":
			if source.Network != "ebpf" {
				return fmt.Errorf("source parameter %q is only supported for ebpf inputs", key)
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("source parameter %q must be a boolean", key)
			}
			source.EBPF.SKBMetadata = &parsed
		case "conntrack":
			if source.Network != "ebpf" {
				return fmt.Errorf("source parameter %q is only supported for ebpf inputs", key)
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("source parameter %q must be a boolean", key)
			}
			source.EBPF.Conntrack = &parsed
		case "conntrack_path":
			if source.Network != "ebpf" {
				return fmt.Errorf("source parameter %q is only supported for ebpf inputs", key)
			}
			if value == "" {
				return fmt.Errorf("source parameter %q cannot be empty", key)
			}
			source.EBPF.ConntrackPath = value
		default:
			return fmt.Errorf("unsupported source parameter %q", key)
		}
	}
	return nil
}

func parseNonNegativeInputParam(name, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("source parameter %q must be an integer", name)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("source parameter %q must be >= 0", name)
	}
	return parsed, nil
}

func parseAggregateParams(cfg *FlagConfig, rawParams string) error {
	rawParams = strings.TrimPrefix(strings.TrimSpace(rawParams), "?")
	if rawParams == "" {
		return fmt.Errorf("parse aggregate parameters: parameters cannot be empty")
	}
	parts := strings.Split(strings.ReplaceAll(rawParams, "&", ","), ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("parse aggregate parameters: parameters cannot be empty")
		}
		key, value, hasValue := strings.Cut(part, "=")
		if !hasValue {
			if err := parseAggregatePreset(cfg, key); err != nil {
				return err
			}
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "idle_flush_after_ms":
			parsed, err := parseNonNegativeAggregateParam(key, value)
			if err != nil {
				return err
			}
			cfg.AggIdleFlushAfter = &parsed
		case "max_flush_after_ms":
			parsed, err := parseNonNegativeAggregateParam(key, value)
			if err != nil {
				return err
			}
			cfg.AggMaxFlushAfter = &parsed
		case "idle_erase_after_ms":
			parsed, err := parseNonNegativeAggregateParam(key, value)
			if err != nil {
				return err
			}
			cfg.AggIdleEraseAfter = &parsed
		case "periodic_every_ms", "every_ms":
			parsed, err := parseNonNegativeAggregateParam(key, value)
			if err != nil {
				return err
			}
			cfg.AggPeriodicEvery = &parsed
		case "reset_buckets":
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("aggregate parameter %q must be a boolean", key)
			}
			cfg.AggResetBuckets = &parsed
		default:
			return fmt.Errorf("unsupported aggregate parameter %q", key)
		}
	}
	return nil
}

func parseAggregatePreset(cfg *FlagConfig, raw string) error {
	preset := strings.ToLower(strings.TrimSpace(raw))
	switch preset {
	case "payload", "header", "packet-header":
		cfg.AggPresets = append(cfg.AggPresets, "payload")
	case "passthrough":
		cfg.AggPresets = append(cfg.AggPresets, "passthrough")
	case "none", "off":
		cfg.AggPresets = append(cfg.AggPresets, "none")
	case "true":
		// Useful when users write -agg=true,payload.
	case "":
		return fmt.Errorf("aggregate preset cannot be empty")
	default:
		return fmt.Errorf("unsupported aggregate preset %q", raw)
	}
	return nil
}

func parseCommaQueryParams(rawParams string) (url.Values, error) {
	rawParams = strings.TrimPrefix(strings.TrimSpace(rawParams), "?")
	if rawParams == "" {
		return nil, fmt.Errorf("parameters cannot be empty")
	}
	return url.ParseQuery(strings.ReplaceAll(rawParams, ",", "&"))
}

func parseNonNegativeAggregateParam(name, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("aggregate parameter %q must be an integer", name)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("aggregate parameter %q must be >= 0", name)
	}
	return parsed, nil
}

func parseOutputSpec(spec string) (EncoderConfig, SinkConfig, error) {
	encoderType, rest, ok := strings.Cut(spec, ":")
	if !ok || encoderType == "" || rest == "" {
		return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: expected encoder:sink[:target]", spec)
	}
	if !supportedEncoderType(encoderType) {
		return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: unsupported encoder %q", spec, encoderType)
	}

	var params url.Values
	if before, rawParams, ok := strings.Cut(rest, "?"); ok {
		if rawParams == "" {
			return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: output parameters cannot be empty", spec)
		}
		parsed, err := parseCommaQueryParams(rawParams)
		if err != nil {
			return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: %w", spec, err)
		}
		rest = before
		params = parsed
	}
	if rest == "" {
		return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: expected encoder:sink[:target]", spec)
	}

	sinkType := rest
	target := ""
	if first, tail, ok := strings.Cut(rest, ":"); ok {
		sinkType = first
		target = tail
	}
	sinkType = normalizeSocketAlias(sinkType)

	sink := SinkConfig{Type: sinkType}
	switch sinkType {
	case "stdout":
		if target != "" {
			return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: stdout does not take a target", spec)
		}
	case "file":
		if target == "" {
			return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: file sink requires a path", spec)
		}
		sink.Path = target
	case "udp":
		if target == "" {
			return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: udp sink requires an address", spec)
		}
		sink.Address = target
	case "unixgram":
		if target == "" {
			return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: unixgram sink requires a path", spec)
		}
		sink.Address = target
	default:
		return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: unsupported sink %q", spec, sinkType)
	}

	encoder := EncoderConfig{Type: encoderType, Workers: AutoWorkers}
	if encoderType == "sflow" {
		encoder.AllowTruncate = boolPtr(true)
	}
	if encoderType == "sflow" || encoderType == "ipfix" {
		encoder.Batch.Enabled = boolPtr(true)
	}
	if err := applyOutputParams(spec, encoderType, params, &encoder); err != nil {
		return EncoderConfig{}, SinkConfig{}, err
	}
	return encoder, sink, nil
}

func applyOutputParams(spec, encoderType string, params url.Values, encoder *EncoderConfig) error {
	for key, values := range params {
		if len(values) == 0 {
			return fmt.Errorf("invalid -output %q: output parameter %q cannot be empty", spec, key)
		}
		value := values[len(values)-1]
		switch key {
		case "allow_truncate":
			if encoderType != "sflow" {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow", spec, key)
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid -output %q: output parameter %q must be a boolean", spec, key)
			}
			encoder.AllowTruncate = boolPtr(parsed)
		case "max_header_bytes":
			if encoderType != "sflow" {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow", spec, key)
			}
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("invalid -output %q: output parameter %q must be an integer", spec, key)
			}
			if parsed < 0 {
				return fmt.Errorf("invalid -output %q: output parameter %q must be >= 0", spec, key)
			}
			encoder.SFlow.MaxHeaderBytes = parsed
		case "batch":
			if !supportsBatchOutputParams(encoderType) {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow and ipfix", spec, key)
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid -output %q: output parameter %q must be a boolean", spec, key)
			}
			encoder.Batch.Enabled = boolPtr(parsed)
		case "batch_max_records":
			if !supportsBatchOutputParams(encoderType) {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow and ipfix", spec, key)
			}
			parsed, err := parseNonNegativeOutputParam(spec, key, value)
			if err != nil {
				return err
			}
			encoder.Batch.MaxRecords = parsed
		case "batch_max_bytes":
			if !supportsBatchOutputParams(encoderType) {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow and ipfix", spec, key)
			}
			parsed, err := parseNonNegativeOutputParam(spec, key, value)
			if err != nil {
				return err
			}
			encoder.Batch.MaxBytes = parsed
		case "batch_flush_interval_ms":
			if !supportsBatchOutputParams(encoderType) {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow and ipfix", spec, key)
			}
			parsed, err := parseNonNegativeOutputParam(spec, key, value)
			if err != nil {
				return err
			}
			encoder.Batch.FlushInterval = parsed
		default:
			return fmt.Errorf("invalid -output %q: unsupported output parameter %q", spec, key)
		}
	}
	return nil
}

func parseNonNegativeOutputParam(spec, key, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid -output %q: output parameter %q must be an integer", spec, key)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("invalid -output %q: output parameter %q must be >= 0", spec, key)
	}
	return parsed, nil
}

func supportsBatchOutputParams(encoderType string) bool {
	switch encoderType {
	case "sflow", "ipfix":
		return true
	default:
		return false
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func validateSocketSourceType(sourceType string) error {
	switch sourceType {
	case "flow", "bytes", "json":
		return nil
	default:
		return fmt.Errorf("socket source type must be flow, bytes, or json")
	}
}

func supportedEncoderType(encoderType string) bool {
	switch encoderType {
	case "json", "protobuf", "sflow", "ipfix", "netflowv9", "netflowv5", "pcap", "pcapng":
		return true
	default:
		return false
	}
}

func validateUDPListenTarget(target string) error {
	if _, err := netip.ParseAddrPort(target); err == nil {
		return nil
	}
	if strings.HasPrefix(target, ":") {
		return nil
	}
	if strings.Contains(target, ":") {
		return nil
	}
	return fmt.Errorf("udp target must include a port")
}

func normalizeSocketAlias(network string) string {
	if network == "socket" {
		return "unixgram"
	}
	return network
}

func cutLast(s, sep string) (string, string, bool) {
	idx := strings.LastIndex(s, sep)
	if idx < 0 {
		return "", "", false
	}
	return s[:idx], s[idx+len(sep):], true
}

func generatedAggregators(flags *FlagConfig) []AggregatorConfig {
	if lastAggregatePreset(flags) == "none" {
		return nil
	}
	cfg := defaultGeneratedAggregator()
	applyGeneratedAggregatorOverrides(&cfg, flags)
	applyAggregationPresetsToAggregator(&cfg, aggregatePresets(flags))
	return []AggregatorConfig{cfg}
}

func defaultGeneratedAggregator() AggregatorConfig {
	return AggregatorConfig{
		Stream: "flow_data",
		Match: map[string]string{
			"record_kind": "packet",
		},
		Window: AggregatorWindowConfig{
			IdleFlushAfter: 10000,
		},
		Periodic: AggregatorPeriodicConfig{
			Every: 60000,
		},
		Fields: []AggregatorField{
			{Role: "key", Name: "src_addr"},
			{Role: "key", Name: "dst_addr"},
			{Role: "key", Name: "proto"},
			{Role: "key", Name: "src_port"},
			{Role: "key", Name: "dst_port"},
			{Role: "sum", Name: "bytes"},
			{Role: "sum", Name: "packets"},
			{Role: "first", Name: "sub_agent_id"},
			{Role: "first", Name: "source_id"},
			{Role: "first", Name: "start_time_unix"},
			{Role: "current", Name: "sub_agent_id"},
			{Role: "current", Name: "source_id"},
			{Role: "current", Name: "input_if"},
			{Role: "current", Name: "output_if"},
			{Role: "current", Name: "end_time_unix"},
			{Role: "current", Name: "mpls_label_stack_section_1"},
			{Role: "current", Name: "mpls_label_stack_section_2"},
			{Role: "current", Name: "mpls_label_stack_section_3"},
		},
		TemplateID: 256,
	}
}

func applyGeneratedAggregatorOverrides(cfg *AggregatorConfig, flags *FlagConfig) {
	if cfg == nil || flags == nil {
		return
	}
	if flags.AggIdleFlushAfter != nil {
		cfg.Window.IdleFlushAfter = *flags.AggIdleFlushAfter
	}
	if flags.AggMaxFlushAfter != nil {
		cfg.Window.MaxFlushAfter = *flags.AggMaxFlushAfter
	}
	if flags.AggIdleEraseAfter != nil {
		cfg.Window.IdleEraseAfter = *flags.AggIdleEraseAfter
	}
	if flags.AggPeriodicEvery != nil {
		cfg.Periodic.Every = *flags.AggPeriodicEvery
	}
	if flags.AggResetBuckets != nil {
		cfg.Periodic.ResetBuckets = *flags.AggResetBuckets
	}
}

// ApplyAggregationFlags applies -agg overlays to an explicit YAML config.
// Presets are useful with -config, while -input/-output remain generated-config
// helpers and are intentionally still rejected with -config.
func (c *Config) ApplyAggregationFlags(flags *FlagConfig) error {
	if c == nil || flags == nil || !flags.Aggregate {
		return nil
	}
	if len(c.Aggregators) == 0 && (len(flags.AggPresets) == 0 || hasAggregateOverrides(flags)) {
		c.Aggregators = []AggregatorConfig{defaultGeneratedAggregator()}
	}
	if len(c.Aggregators) > 0 {
		applyGeneratedAggregatorOverrides(&c.Aggregators[0], flags)
	}
	for _, preset := range aggregatePresets(flags) {
		switch preset {
		case "none":
			c.Aggregators = nil
		case "passthrough", "payload":
			agg := c.ensurePrimaryAggregator()
			applyAggregationPresetsToAggregator(agg, []string{preset})
		}
	}
	for i := range c.Aggregators {
		if c.Aggregators[i].Stream == "" {
			c.Aggregators[i].Stream = "flow_data"
		}
		if err := validateAggregatorFields(c.Aggregators[i].Fields); err != nil {
			return fmt.Errorf("aggregators[%d]: %w", i, err)
		}
		c.Aggregators[i].Passthrough = !aggregatorNeedsState(&c.Aggregators[i])
		if err := validateAggregatorConfig(c.Aggregators[i]); err != nil {
			return fmt.Errorf("aggregators[%d]: %w", i, err)
		}
	}
	return nil
}

func (c *Config) ensurePrimaryAggregator() *AggregatorConfig {
	if len(c.Aggregators) == 0 {
		c.Aggregators = []AggregatorConfig{defaultGeneratedAggregator()}
	}
	return &c.Aggregators[0]
}

func applyAggregationPresetsToAggregator(cfg *AggregatorConfig, presets []string) {
	for _, preset := range presets {
		switch preset {
		case "none":
			// Handled at Config level.
		case "passthrough":
			makeAggregatorPassthrough(cfg)
		case "payload":
			addPayloadFields(cfg)
		}
	}
}

func makeAggregatorPassthrough(cfg *AggregatorConfig) {
	if cfg == nil {
		return
	}
	cfg.Window = AggregatorWindowConfig{}
	cfg.Periodic = AggregatorPeriodicConfig{}
	cfg.ResetInterval = 0
	cfg.PeriodicInterval = 0
	cfg.Sum = nil
	cfg.First = nil
	cfg.Min = nil
	cfg.Max = nil
	if len(cfg.Fields) > 0 {
		out := cfg.Fields[:0]
		for _, field := range cfg.Fields {
			switch field.Role {
			case "key", "current", "static":
				out = append(out, field)
			}
		}
		cfg.Fields = out
	}
}

func addPayloadFields(cfg *AggregatorConfig) {
	if cfg == nil {
		return
	}
	appendUniqueAggregatorField(cfg, AggregatorField{Role: "current", Name: "frame_length"})
	appendUniqueAggregatorField(cfg, AggregatorField{Role: "current", Name: "header_data"})
	appendUniqueString(&cfg.Current, "frame_length")
	appendUniqueString(&cfg.Current, "header_data")
}

func appendUniqueAggregatorField(cfg *AggregatorConfig, field AggregatorField) {
	for _, existing := range cfg.Fields {
		if existing.Role == field.Role && existing.Name == field.Name {
			return
		}
	}
	cfg.Fields = append(cfg.Fields, field)
}

func appendUniqueString(values *[]string, value string) {
	for _, existing := range *values {
		if existing == value {
			return
		}
	}
	*values = append(*values, value)
}

func aggregatePresets(flags *FlagConfig) []string {
	if flags == nil {
		return nil
	}
	return flags.AggPresets
}

func hasAggregateOverrides(flags *FlagConfig) bool {
	return flags != nil && (flags.AggIdleFlushAfter != nil || flags.AggMaxFlushAfter != nil || flags.AggIdleEraseAfter != nil || flags.AggPeriodicEvery != nil || flags.AggResetBuckets != nil)
}

func aggregatePresetRequested(flags *FlagConfig, want string) bool {
	for _, preset := range aggregatePresets(flags) {
		if preset == want {
			return true
		}
	}
	return false
}

func lastAggregatePreset(flags *FlagConfig) string {
	presets := aggregatePresets(flags)
	if len(presets) == 0 {
		return ""
	}
	return presets[len(presets)-1]
}

func validateAggregatorFields(fields []AggregatorField) error {
	for _, field := range fields {
		if err := validateAggregatorField(field); err != nil {
			return err
		}
	}
	return nil
}
