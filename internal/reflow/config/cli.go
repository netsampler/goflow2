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
		"ebpf:<interface>:bytes[?snaplen=<bytes>&sample_every=<n>&sample_offset=<n>&direction=ingress|egress|both&skb_metadata=<bool>&conntrack=<bool>]",
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
		"sflow|ipfix|netflowv9:*[?batch=<bool>&batch_max_records=<n>&batch_max_bytes=<bytes>&batch_flush_interval_ms=<ms>]",
	}
	inputHelperExamples = []string{
		"udp::6343:flow",
		"udp:127.0.0.1:2055:flow",
		"stream:-:json",
		"'ebpf:eth0:bytes?direction=ingress'",
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
		"-agg payload",
		"-agg payload,limited",
		"-agg nat",
		"-agg encap",
		"-agg mpls",
		"-agg passthrough",
		"-agg idle_flush_after_ms=<ms>,periodic_every_ms=<ms>",
		"-agg max_flush_after_ms=<ms>,idle_erase_after_ms=<ms>,reset_buckets=<bool>",
	}
	aggregateHelperExamples = []string{
		"-agg payload",
		"-agg payload,limited",
		"-agg payload,limited,nat",
		"-agg payload,limited,encap",
		"-agg nat",
		"-agg encap",
		"-agg mpls",
		"-agg passthrough",
		"-agg idle_flush_after_ms=5000,periodic_every_ms=30000",
		"-agg periodic_every_ms=10000,reset_buckets=true",
	}
	generatedTemplatedFlowFields = []string{
		"bytes",
		"packets",
		"proto",
		"tcp_flags",
		"src_port",
		"src_addr",
		"input_if",
		"dst_port",
		"dst_addr",
		"output_if",
		"src_mac",
		"vlan_id",
		"flow_direction",
		"dst_mac",
		"agent_ip",
		"agent_ipv6",
		"source_id",
		"start_time_unix",
		"end_time_unix",
		"ether_type",
	}
	generatedTemplatedFlowMPLSFields = []string{
		"mpls_label_stack_section_1",
		"mpls_label_stack_section_2",
		"mpls_label_stack_section_3",
	}
	generatedTemplatedFlowNATFields = []string{
		"nat_src_addr",
		"nat_dst_addr",
		"nat_src_port",
		"nat_dst_port",
	}
	generatedTemplatedFlowEncapFields = []string{
		"outer_proto",
		"outer_proto_name",
		"outer_src_port",
		"outer_src_addr",
		"outer_dst_port",
		"outer_dst_addr",
		"encap_depth",
	}
	generatedPacketParsedFlowFields = []string{
		"protocol",
		"header_protocol",
		"header_protocol_name",
		"proto",
		"proto_name",
		"tcp_flags",
		"src_port",
		"src_addr",
		"dst_port",
		"dst_addr",
		"ip_family",
		"src_mac",
		"vlan_id",
		"dst_mac",
		"ether_type",
		"outer_proto",
		"outer_proto_name",
		"outer_src_port",
		"outer_src_addr",
		"outer_dst_port",
		"outer_dst_addr",
		"encap_depth",
	}
	generatedPacketParsedIPFields = map[string]struct{}{
		"src_addr":  {},
		"dst_addr":  {},
		"ip_family": {},
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
	if encoder.Type == "ipfix" || encoder.Type == "netflowv9" {
		cfg.Encoder.TemplatedFlow.Data.Select = append([]string(nil), generatedTemplatedFlowFields...)
	}
	if c.Aggregate {
		cfg.Aggregators = generatedAggregators(c)
		if lastAggregatePreset(c) != "none" && aggregatePresetRequested(c, "payload") {
			cfg.ensureTemplatedFlowFieldsSelected("frame_length", "header_data")
		}
		if lastAggregatePreset(c) != "none" && aggregatePresetRequested(c, "nat") {
			cfg.ensureTemplatedFlowFieldsSelected(generatedTemplatedFlowNATFields...)
		}
		if lastAggregatePreset(c) != "none" && aggregatePresetRequested(c, "encap") {
			cfg.enablePacketDecoderBeyondL4()
			cfg.ensureTemplatedFlowFieldsSelected(generatedTemplatedFlowEncapFields...)
		}
		if lastAggregatePreset(c) != "none" && aggregatePresetRequested(c, "mpls") {
			cfg.enableMPLSAggregationHelpers()
			cfg.ensureTemplatedFlowFieldsSelected(generatedTemplatedFlowMPLSFields...)
		}
		if lastAggregatePreset(c) != "none" && aggregatePresetRequested(c, "limited") {
			cfg.removeTemplatedFlowFieldsSelected(packetParsedFieldsToRemove(
				aggregatePresetRequested(c, "nat"),
				aggregatePresetRequested(c, "encap"),
			)...)
		}
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
			if c.Sources[i].EBPF.Direction == "" {
				c.Sources[i].EBPF.Direction = "both"
			}
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
		case "direction":
			if source.Network != "ebpf" {
				return fmt.Errorf("source parameter %q is only supported for ebpf inputs", key)
			}
			parsed, err := NormalizeEBPFDirection(value)
			if err != nil {
				return fmt.Errorf("source parameter %q: %w", key, err)
			}
			source.EBPF.Direction = parsed
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
	case "nat":
		cfg.AggPresets = append(cfg.AggPresets, "nat")
	case "encap", "encapsulation", "outer":
		cfg.AggPresets = append(cfg.AggPresets, "encap")
	case "mpls":
		cfg.AggPresets = append(cfg.AggPresets, "mpls")
	case "limited", "no-packet-fields", "no-packet", "strip-packet-fields", "packetless":
		cfg.AggPresets = append(cfg.AggPresets, "limited")
	case "payload-only":
		cfg.AggPresets = append(cfg.AggPresets, "payload", "limited")
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

// NormalizeAggregateArgs lets -agg accept either -agg=value or -agg value while
// keeping bare -agg as the existing boolean shorthand.
func NormalizeAggregateArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg == "-agg" || arg == "--agg") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			out = append(out, arg+"="+args[i+1])
			i++
			continue
		}
		out = append(out, arg)
	}
	return out
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
	if encoderType == "sflow" || encoderType == "ipfix" || encoderType == "netflowv9" {
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
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow, ipfix, and netflowv9", spec, key)
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("invalid -output %q: output parameter %q must be a boolean", spec, key)
			}
			encoder.Batch.Enabled = boolPtr(parsed)
		case "batch_max_records":
			if !supportsBatchOutputParams(encoderType) {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow, ipfix, and netflowv9", spec, key)
			}
			parsed, err := parseNonNegativeOutputParam(spec, key, value)
			if err != nil {
				return err
			}
			encoder.Batch.MaxRecords = parsed
		case "batch_max_bytes":
			if !supportsBatchOutputParams(encoderType) {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow, ipfix, and netflowv9", spec, key)
			}
			parsed, err := parseNonNegativeOutputParam(spec, key, value)
			if err != nil {
				return err
			}
			encoder.Batch.MaxBytes = parsed
		case "batch_flush_interval_ms":
			if !supportsBatchOutputParams(encoderType) {
				return fmt.Errorf("invalid -output %q: output parameter %q is only supported for sflow, ipfix, and netflowv9", spec, key)
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
	case "sflow", "ipfix", "netflowv9":
		return true
	default:
		return false
	}
}

func boolPtr(v bool) *bool {
	return &v
}

func uint32Ptr(v uint32) *uint32 {
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
			{Role: "sum", Name: "bytes"},
			{Role: "sum", Name: "packets"},
			{Role: "key", Name: "proto"},
			{Role: "and", Name: "tcp_flags"},
			{Role: "key", Name: "src_port"},
			{Role: "key", Name: "src_addr"},
			{Role: "current", Name: "input_if"},
			{Role: "key", Name: "dst_port"},
			{Role: "key", Name: "dst_addr"},
			{Role: "current", Name: "output_if"},
			{Role: "current", Name: "src_mac"},
			{Role: "current", Name: "vlan_id"},
			{Role: "current", Name: "flow_direction"},
			{Role: "current", Name: "dst_mac"},
			{Role: "current", Name: "agent_ip"},
			{Role: "current", Name: "agent_ipv6"},
			{Role: "key", Name: "source_id"},
			{Role: "first", Name: "start_time_unix"},
			{Role: "current", Name: "end_time_unix"},
			{Role: "current", Name: "ether_type"},
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
		case "passthrough", "payload", "nat", "encap", "mpls", "limited":
			agg := c.ensurePrimaryAggregator()
			if preset == "limited" {
				removePacketParsedFields(agg,
					aggregatePresetRequested(flags, "nat"),
					aggregatePresetRequested(flags, "encap"),
				)
			} else {
				applyAggregationPresetsToAggregator(agg, []string{preset})
			}
			if preset == "payload" {
				c.ensureTemplatedFlowFieldsSelected("frame_length", "header_data")
			}
			if preset == "nat" {
				c.ensureTemplatedFlowFieldsSelected(generatedTemplatedFlowNATFields...)
			}
			if preset == "encap" {
				c.enablePacketDecoderBeyondL4()
				c.ensureTemplatedFlowFieldsSelected(generatedTemplatedFlowEncapFields...)
			}
			if preset == "mpls" {
				c.enableMPLSAggregationHelpers()
				c.ensureTemplatedFlowFieldsSelected(generatedTemplatedFlowMPLSFields...)
			}
			if preset == "limited" {
				c.removeTemplatedFlowFieldsSelected(packetParsedFieldsToRemove(
					aggregatePresetRequested(flags, "nat"),
					aggregatePresetRequested(flags, "encap"),
				)...)
			}
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
	keepIPFields := aggregatePresetSliceContains(presets, "nat")
	keepEncapFields := aggregatePresetSliceContains(presets, "encap")
	for _, preset := range presets {
		switch preset {
		case "none":
			// Handled at Config level.
		case "passthrough":
			makeAggregatorPassthrough(cfg)
		case "payload":
			addPayloadFields(cfg)
		case "nat":
			addNATFields(cfg)
		case "encap":
			addEncapFields(cfg)
		case "mpls":
			addMPLSFields(cfg)
		case "limited":
			removePacketParsedFields(cfg, keepIPFields, keepEncapFields)
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
	cfg.And = nil
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

func addNATFields(cfg *AggregatorConfig) {
	if cfg == nil {
		return
	}
	for _, name := range generatedTemplatedFlowNATFields {
		appendUniqueAggregatorField(cfg, AggregatorField{Role: "current", Name: name})
		appendUniqueString(&cfg.Current, name)
	}
}

func addEncapFields(cfg *AggregatorConfig) {
	if cfg == nil {
		return
	}
	for _, name := range []string{"outer_proto", "outer_src_port", "outer_src_addr", "outer_dst_port", "outer_dst_addr"} {
		appendUniqueAggregatorField(cfg, AggregatorField{Role: "key", Name: name})
		appendUniqueString(&cfg.KeyFields, name)
	}
	for _, name := range []string{"outer_proto_name", "encap_depth"} {
		appendUniqueAggregatorField(cfg, AggregatorField{Role: "current", Name: name})
		appendUniqueString(&cfg.Current, name)
	}
}

func addMPLSFields(cfg *AggregatorConfig) {
	if cfg == nil {
		return
	}
	for _, name := range generatedTemplatedFlowMPLSFields {
		appendUniqueAggregatorField(cfg, AggregatorField{Role: "current", Name: name})
		appendUniqueString(&cfg.Current, name)
	}
}

func removePacketParsedFields(cfg *AggregatorConfig, keepIPFields, keepEncapFields bool) {
	if cfg == nil {
		return
	}
	remove := packetParsedFieldSet(keepIPFields, keepEncapFields)
	cfg.Fields = filterAggregatorFieldsByName(cfg.Fields, remove)
	cfg.KeyFields = filterStringsBySet(cfg.KeyFields, remove)
	cfg.Current = filterStringsBySet(cfg.Current, remove)
	cfg.Sum = filterStringsBySet(cfg.Sum, remove)
	cfg.First = filterStringsBySet(cfg.First, remove)
	cfg.Min = filterStringsBySet(cfg.Min, remove)
	cfg.Max = filterStringsBySet(cfg.Max, remove)
	for name := range remove {
		delete(cfg.StaticFields, name)
	}
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

func (c *Config) enableMPLSAggregationHelpers() {
	if c == nil {
		return
	}
	if c.Processor.Builtin.AggregationHelpers.MPLSLabels < len(generatedTemplatedFlowMPLSFields) {
		c.Processor.Builtin.AggregationHelpers.MPLSLabels = len(generatedTemplatedFlowMPLSFields)
	}
}

func (c *Config) enablePacketDecoderBeyondL4() {
	if c == nil {
		return
	}
	c.Processor.Builtin.PacketDecoder.DecodeBeyondL4 = boolPtr(true)
}

func (c *Config) ensureTemplatedFlowFieldsSelected(names ...string) {
	if c == nil || (c.Encoder.Type != "ipfix" && c.Encoder.Type != "netflowv9") || len(c.Encoder.TemplatedFlow.Data.Select) == 0 {
		return
	}
	for _, name := range names {
		appendUniqueString(&c.Encoder.TemplatedFlow.Data.Select, name)
	}
}

func (c *Config) removeTemplatedFlowFieldsSelected(names ...string) {
	if c == nil || len(c.Encoder.TemplatedFlow.Data.Select) == 0 {
		return
	}
	remove := make(map[string]struct{}, len(names))
	for _, name := range names {
		remove[name] = struct{}{}
	}
	c.Encoder.TemplatedFlow.Data.Select = filterStringsBySet(c.Encoder.TemplatedFlow.Data.Select, remove)
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
	return aggregatePresetSliceContains(aggregatePresets(flags), want)
}

func lastAggregatePreset(flags *FlagConfig) string {
	presets := aggregatePresets(flags)
	if len(presets) == 0 {
		return ""
	}
	return presets[len(presets)-1]
}

func aggregatePresetSliceContains(presets []string, want string) bool {
	for _, preset := range presets {
		if preset == want {
			return true
		}
	}
	return false
}

func packetParsedFieldsToRemove(keepIPFields, keepEncapFields bool) []string {
	fields := make([]string, 0, len(generatedPacketParsedFlowFields))
	for _, name := range generatedPacketParsedFlowFields {
		if keepIPFields {
			if _, ok := generatedPacketParsedIPFields[name]; ok {
				continue
			}
		}
		if keepEncapFields {
			if stringInSlice(generatedTemplatedFlowEncapFields, name) {
				continue
			}
		}
		fields = append(fields, name)
	}
	return fields
}

func packetParsedFieldSet(keepIPFields, keepEncapFields bool) map[string]struct{} {
	fields := packetParsedFieldsToRemove(keepIPFields, keepEncapFields)
	out := make(map[string]struct{}, len(fields))
	for _, name := range fields {
		out[name] = struct{}{}
	}
	return out
}

func stringInSlice(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func filterAggregatorFieldsByName(fields []AggregatorField, remove map[string]struct{}) []AggregatorField {
	if len(fields) == 0 || len(remove) == 0 {
		return fields
	}
	out := fields[:0]
	for _, field := range fields {
		if _, ok := remove[field.Name]; ok {
			continue
		}
		out = append(out, field)
	}
	return out
}

func filterStringsBySet(values []string, remove map[string]struct{}) []string {
	if len(values) == 0 || len(remove) == 0 {
		return values
	}
	out := values[:0]
	for _, value := range values {
		if _, ok := remove[value]; ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func validateAggregatorFields(fields []AggregatorField) error {
	for _, field := range fields {
		if err := validateAggregatorField(field); err != nil {
			return err
		}
	}
	return nil
}
