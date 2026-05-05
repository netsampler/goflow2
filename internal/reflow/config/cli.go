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
		"ebpf:<interface>:bytes[?snaplen=<bytes>&sample_every=<n>&sample_offset=<n>]",
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
	b.WriteString("\nOutput examples:\n")
	for _, example := range outputHelperExamples {
		fmt.Fprintf(&b, "  -output %s\n", example)
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

// LoadFromFlags resolves either explicit YAML mode or generated-helper mode.
func LoadFromFlags(flags *FlagConfig) (*Config, bool, error) {
	if flags == nil {
		flags = &FlagConfig{}
	}
	if flags.ConfigPath != "" {
		if flags.usesGeneratedHelpers() {
			return nil, false, fmt.Errorf("-config cannot be combined with -input, -output/-o, -agg, or -genconf")
		}
		cfg, err := Load(flags.ConfigPath)
		return cfg, false, err
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
			Workers: 2,
			Builtin: BuiltinProcessorConfig{
				DropMessage: true,
				DropPayload: true,
			},
		},
		Encoder: encoder,
		Sink:    sink,
	}
	if c.Aggregate {
		cfg.Aggregators = generatedAggregators()
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
	}
	defaultFalse(&c.Processor.Builtin.PacketDecoder.DecodeBeyondL4)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.GRE.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.IPIP.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.VXLAN.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.Geneve.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.L2TP.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.GTPU.Enabled)
	defaultFalse(&c.Processor.Builtin.PacketDecoder.Encapsulations.PPPoE.Enabled)
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

func parseOutputSpec(spec string) (EncoderConfig, SinkConfig, error) {
	encoderType, rest, ok := strings.Cut(spec, ":")
	if !ok || encoderType == "" || rest == "" {
		return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: expected encoder:sink[:target]", spec)
	}
	if !supportedEncoderType(encoderType) {
		return EncoderConfig{}, SinkConfig{}, fmt.Errorf("invalid -output %q: unsupported encoder %q", spec, encoderType)
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

	return EncoderConfig{Type: encoderType}, sink, nil
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

func generatedAggregators() []AggregatorConfig {
	base := func(family string, templateID uint16) AggregatorConfig {
		return AggregatorConfig{
			Stream: "flow_data_" + family,
			Match: map[string]string{
				"ip_family": family,
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
				{Role: "first", Name: "agent_ip"},
				{Role: "first", Name: "sub_agent_id"},
				{Role: "first", Name: "source_id"},
				{Role: "first", Name: "start_time_unix"},
				{Role: "current", Name: "agent_ip"},
				{Role: "current", Name: "sub_agent_id"},
				{Role: "current", Name: "source_id"},
				{Role: "current", Name: "input_if"},
				{Role: "current", Name: "output_if"},
				{Role: "current", Name: "sampling_rate"},
				{Role: "current", Name: "sample_pool"},
				{Role: "current", Name: "drops"},
				{Role: "current", Name: "end_time_unix"},
				{Role: "current", Name: "mpls_label1"},
				{Role: "current", Name: "mpls_label2"},
				{Role: "current", Name: "mpls_label3"},
			},
			TemplateID: templateID,
		}
	}
	return []AggregatorConfig{
		base("ipv4", 256),
		base("ipv6", 258),
	}
}
