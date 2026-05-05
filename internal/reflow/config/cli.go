package config

import (
	"fmt"
	"net/netip"
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
		"ebpf:<interface>:bytes",
		"pcap_live:<interface>:bytes",
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
		"pcap_live:en0:bytes",
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
	return source, nil
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
			KeyFields: []string{
				"src_addr",
				"dst_addr",
				"proto",
				"src_port",
				"dst_port",
			},
			Sum: []string{
				"bytes",
				"packets",
			},
			First: []string{
				"agent_ip",
				"sub_agent_id",
				"source_id",
				"start_time_unix",
			},
			Current: []string{
				"agent_ip",
				"sub_agent_id",
				"source_id",
				"input_if",
				"output_if",
				"sampling_rate",
				"sample_pool",
				"drops",
				"end_time_unix",
				"mpls_label1",
				"mpls_label2",
				"mpls_label3",
			},
			TemplateID: templateID,
		}
	}
	return []AggregatorConfig{
		base("ipv4", 256),
		base("ipv6", 258),
	}
}
