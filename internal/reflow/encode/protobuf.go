package encode

import (
	"fmt"
	"net/netip"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	flowpb "github.com/netsampler/goflow2/v3/pb"
	"google.golang.org/protobuf/encoding/protowire"
)

type protobufFieldSpec struct {
	name   string
	append func([]byte, *event.Event) ([]byte, bool, error)
}

type ProtobufEncoder struct {
	flavor         string
	lengthPrefixed bool
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
		fields:         fields,
	}, nil
}

func (e *ProtobufEncoder) Encode(evt *event.Event) ([][]byte, error) {
	buf := make([]byte, 0, 256)
	for _, field := range e.fields {
		var ok bool
		var err error
		buf, ok, err = field.append(buf, evt)
		if err != nil {
			return nil, fmt.Errorf("encode protobuf field %s: %w", field.name, err)
		}
		if !ok {
			continue
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
		name: name,
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
		name: name,
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
		name: name,
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
		name: name,
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
