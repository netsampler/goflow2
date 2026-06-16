package encode

import (
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"sort"
	"strings"

	flowpb "github.com/netsampler/goflow2/v3/pb"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"google.golang.org/protobuf/encoding/protowire"
)

type protobufFieldSpec struct {
	name      string
	number    protowire.Number
	protoType string
	append    func([]byte, *event.Event) ([]byte, bool, error)
}

type ProtobufEncoder struct {
	flavor         string
	lengthPrefixed bool
	exportAll      bool
	fields         []protobufFieldSpec
}

func NewProtobufEncoder(cfg config.EncoderConfig) (*ProtobufEncoder, error) {
	fields, err := compileProtobufFieldPlan(cfg.Protobuf.Flavor)
	if err != nil {
		return nil, err
	}
	return &ProtobufEncoder{
		flavor:         cfg.Protobuf.Flavor,
		lengthPrefixed: cfg.Protobuf.LengthPrefixed,
		exportAll:      cfg.Protobuf.ExportAll,
		fields:         fields,
	}, nil
}

func (e *ProtobufEncoder) Encode(evt *event.Event) ([][]byte, error) {
	buf := make([]byte, 0, 256)
	for _, field := range e.fields {
		next, ok, err := field.append(buf, evt)
		if err != nil {
			return nil, fmt.Errorf("encode protobuf field %s: %w", field.name, err)
		}
		if !ok {
			continue
		}
		buf = next
	}
	if e.exportAll {
		var err error
		buf, err = appendProtobufExtraFields(buf, evt)
		if err != nil {
			return nil, err
		}
	}

	if e.lengthPrefixed {
		framed := protowire.AppendVarint(make([]byte, 0, len(buf)+10), uint64(len(buf)))
		framed = append(framed, buf...)
		return [][]byte{framed}, nil
	}
	return [][]byte{buf}, nil
}

func (e *ProtobufEncoder) Flush() ([][]byte, error) {
	return nil, nil
}

func GenerateProtobufDefinition(cfg config.ProtobufConfig) (string, error) {
	fields, err := compileProtobufFieldPlan(cfg.Flavor)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\n")
	b.WriteString("package flowpb;\n")
	b.WriteString("option go_package = \"github.com/netsampler/goflow2/pb;flowpb\";\n\n")
	b.WriteString("message FlowMessage {\n\n")
	b.WriteString("  enum FlowType {\n")
	b.WriteString("    FLOWUNKNOWN = 0;\n")
	b.WriteString("    SFLOW_5 = 1;\n")
	b.WriteString("    NETFLOW_V5 = 2;\n")
	b.WriteString("    NETFLOW_V9 = 3;\n")
	b.WriteString("    IPFIX = 4;\n")
	b.WriteString("  }\n\n")
	for _, field := range fields {
		fmt.Fprintf(&b, "  %s %s = %d;\n", field.protoType, field.name, field.number)
	}
	if cfg.ExportAll {
		b.WriteString("  repeated ExtraField extra = 1000;\n\n")
		b.WriteString("  message ExtraField {\n")
		b.WriteString("    string key = 1;\n")
		b.WriteString("    oneof value {\n")
		b.WriteString("      string string_value = 2;\n")
		b.WriteString("      uint64 uint64_value = 3;\n")
		b.WriteString("      sint64 int64_value = 4;\n")
		b.WriteString("      bool bool_value = 5;\n")
		b.WriteString("      bytes bytes_value = 6;\n")
		b.WriteString("      double double_value = 7;\n")
		b.WriteString("      string json_value = 8;\n")
		b.WriteString("    }\n")
		b.WriteString("  }\n")
	}
	b.WriteString("\n}\n")
	return b.String(), nil
}

func compileProtobufFieldPlan(flavor string) ([]protobufFieldSpec, error) {
	switch flavor {
	case "", "canonical", "goflow2v2":
		return []protobufFieldSpec{
			protobufEnumField("type", 1, protobufFlowTypeValue),
			protobufUint64Field("time_received_ns", 110, func(evt *event.Event) uint64 {
				if evt == nil || evt.ReceivedAt.IsZero() {
					return 0
				}
				return uint64(evt.ReceivedAt.UnixNano())
			}),
			protobufUint32Field("sequence_num", 4, func(evt *event.Event) uint32 {
				if evt != nil && evt.SFlow != nil && evt.SFlow.SequenceNumber != 0 {
					return evt.SFlow.SequenceNumber
				}
				return uint32Field(evt.Fields, "sequence_num")
			}),
			protobufUint64Field("sampling_rate", 3, func(evt *event.Event) uint64 {
				return uint64(eventSamplingRate(evt))
			}),
			protobufIPField("sampler_address", 11, func(evt *event.Event) string {
				return eventAgentIP(evt)
			}),
			protobufUint64Field("time_flow_start_ns", 111, func(evt *event.Event) uint64 {
				startNS := timeFlowNS(evt.Fields, "time_flow_start_ns", "start_time_unix")
				if startNS == 0 {
					return 0
				}
				return uint64(startNS)
			}),
			protobufUint64Field("time_flow_end_ns", 112, func(evt *event.Event) uint64 {
				endNS := timeFlowNS(evt.Fields, "time_flow_end_ns", "end_time_unix")
				if endNS == 0 {
					return 0
				}
				return uint64(endNS)
			}),
			protobufUint64Field("bytes", 9, func(evt *event.Event) uint64 {
				return uint64Field(evt.Fields, "bytes")
			}),
			protobufUint64Field("packets", 10, func(evt *event.Event) uint64 {
				return uint64Field(evt.Fields, "packets")
			}),
			protobufIPField("src_addr", 6, func(evt *event.Event) string {
				return stringFieldOrZero(evt.Fields, "src_addr")
			}),
			protobufIPField("dst_addr", 7, func(evt *event.Event) string {
				return stringFieldOrZero(evt.Fields, "dst_addr")
			}),
			protobufUint32Field("etype", 30, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "etype")
			}),
			protobufUint32Field("proto", 20, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "proto")
			}),
			protobufUint32Field("src_port", 21, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "src_port")
			}),
			protobufUint32Field("dst_port", 22, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "dst_port")
			}),
			protobufUint32Field("in_if", 18, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "input_if")
			}),
			protobufUint32Field("out_if", 19, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "output_if")
			}),
			protobufUint32Field("observation_domain_id", 70, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "observation_domain_id")
			}),
			protobufUint32Field("observation_point_id", 71, func(evt *event.Event) uint32 {
				return uint32Field(evt.Fields, "observation_point_id")
			}),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported protobuf flavor %q", flavor)
	}
}

func protobufUint32Field(name string, num protowire.Number, get func(*event.Event) uint32) protobufFieldSpec {
	return protobufFieldSpec{
		name:      name,
		number:    num,
		protoType: "uint32",
		append: func(dst []byte, evt *event.Event) ([]byte, bool, error) {
			val := get(evt)
			if val == 0 {
				return dst, false, nil
			}
			dst = protowire.AppendTag(dst, num, protowire.VarintType)
			dst = protowire.AppendVarint(dst, uint64(val))
			return dst, true, nil
		},
	}
}

func protobufUint64Field(name string, num protowire.Number, get func(*event.Event) uint64) protobufFieldSpec {
	return protobufFieldSpec{
		name:      name,
		number:    num,
		protoType: "uint64",
		append: func(dst []byte, evt *event.Event) ([]byte, bool, error) {
			val := get(evt)
			if val == 0 {
				return dst, false, nil
			}
			dst = protowire.AppendTag(dst, num, protowire.VarintType)
			dst = protowire.AppendVarint(dst, val)
			return dst, true, nil
		},
	}
}

func protobufIPField(name string, num protowire.Number, get func(*event.Event) string) protobufFieldSpec {
	return protobufFieldSpec{
		name:      name,
		number:    num,
		protoType: "bytes",
		append: func(dst []byte, evt *event.Event) ([]byte, bool, error) {
			ip := get(evt)
			if ip == "" {
				return dst, false, nil
			}
			addr, err := netip.ParseAddr(ip)
			if err != nil {
				return dst, false, nil
			}
			dst = protowire.AppendTag(dst, num, protowire.BytesType)
			dst = protowire.AppendBytes(dst, addr.AsSlice())
			return dst, true, nil
		},
	}
}

func protobufEnumField(name string, num protowire.Number, get func(*event.Event) uint64) protobufFieldSpec {
	return protobufFieldSpec{
		name:      name,
		number:    num,
		protoType: "FlowType",
		append: func(dst []byte, evt *event.Event) ([]byte, bool, error) {
			val := get(evt)
			if val == 0 {
				return dst, false, nil
			}
			dst = protowire.AppendTag(dst, num, protowire.VarintType)
			dst = protowire.AppendVarint(dst, val)
			return dst, true, nil
		},
	}
}

func appendProtobufExtraFields(dst []byte, evt *event.Event) ([]byte, error) {
	if evt == nil || len(evt.Fields) == 0 {
		return dst, nil
	}
	keys := make([]string, 0, len(evt.Fields))
	for key := range evt.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		msg, ok, err := protobufExtraFieldMessage(key, evt.Fields[key])
		if err != nil {
			return nil, fmt.Errorf("encode protobuf extra field %s: %w", key, err)
		}
		if !ok {
			continue
		}
		dst = protowire.AppendTag(dst, 1000, protowire.BytesType)
		dst = protowire.AppendBytes(dst, msg)
	}
	return dst, nil
}

func protobufExtraFieldMessage(key string, val any) ([]byte, bool, error) {
	if key == "" {
		return nil, false, nil
	}
	msg := protowire.AppendTag(nil, 1, protowire.BytesType)
	msg = protowire.AppendString(msg, key)
	return appendProtobufExtraValue(msg, val)
}

func appendProtobufExtraValue(dst []byte, val any) ([]byte, bool, error) {
	switch v := val.(type) {
	case nil:
		return appendProtobufExtraJSONValue(dst, nil)
	case string:
		dst = protowire.AppendTag(dst, 2, protowire.BytesType)
		dst = protowire.AppendString(dst, v)
	case []byte:
		dst = protowire.AppendTag(dst, 6, protowire.BytesType)
		dst = protowire.AppendBytes(dst, v)
	case bool:
		dst = protowire.AppendTag(dst, 5, protowire.VarintType)
		dst = protowire.AppendVarint(dst, protowire.EncodeBool(v))
	case int:
		dst = appendProtobufExtraInt64Value(dst, int64(v))
	case int8:
		dst = appendProtobufExtraInt64Value(dst, int64(v))
	case int16:
		dst = appendProtobufExtraInt64Value(dst, int64(v))
	case int32:
		dst = appendProtobufExtraInt64Value(dst, int64(v))
	case int64:
		dst = appendProtobufExtraInt64Value(dst, v)
	case uint:
		dst = appendProtobufExtraUint64Value(dst, uint64(v))
	case uint8:
		dst = appendProtobufExtraUint64Value(dst, uint64(v))
	case uint16:
		dst = appendProtobufExtraUint64Value(dst, uint64(v))
	case uint32:
		dst = appendProtobufExtraUint64Value(dst, uint64(v))
	case uint64:
		dst = appendProtobufExtraUint64Value(dst, v)
	case float32:
		dst = appendProtobufExtraDoubleValue(dst, float64(v))
	case float64:
		dst = appendProtobufExtraDoubleValue(dst, v)
	default:
		return appendProtobufExtraJSONValue(dst, v)
	}
	return dst, true, nil
}

func appendProtobufExtraUint64Value(dst []byte, val uint64) []byte {
	dst = protowire.AppendTag(dst, 3, protowire.VarintType)
	return protowire.AppendVarint(dst, val)
}

func appendProtobufExtraInt64Value(dst []byte, val int64) []byte {
	dst = protowire.AppendTag(dst, 4, protowire.VarintType)
	return protowire.AppendVarint(dst, protowire.EncodeZigZag(val))
}

func appendProtobufExtraDoubleValue(dst []byte, val float64) []byte {
	dst = protowire.AppendTag(dst, 7, protowire.Fixed64Type)
	return protowire.AppendFixed64(dst, math.Float64bits(val))
}

func appendProtobufExtraJSONValue(dst []byte, val any) ([]byte, bool, error) {
	raw, err := json.Marshal(val)
	if err != nil {
		raw, err = json.Marshal(fmt.Sprint(val))
		if err != nil {
			return nil, false, err
		}
	}
	dst = protowire.AppendTag(dst, 8, protowire.BytesType)
	dst = protowire.AppendBytes(dst, raw)
	return dst, true, nil
}

func protobufFlowTypeValue(evt *event.Event) uint64 {
	if evt == nil {
		return 0
	}
	if evt.Fields != nil {
		switch stringFieldOrZero(evt.Fields, "counter_type") {
		case "sflow":
			return uint64(flowpb.FlowMessage_SFLOW_5)
		}
		switch val := flowTypeField(evt.Fields).(type) {
		case int:
			return uint64(val)
		case uint32:
			return uint64(val)
		case uint64:
			return val
		}
	}
	if evt.SFlow != nil {
		return uint64(flowpb.FlowMessage_SFLOW_5)
	}
	return 0
}
