package flowpb

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	corepb "github.com/netsampler/goflow2/v2/pb"
)

// The enricher decodes the FlowMessage produced by goflow2 as a
// FlowMessageExt, so every FlowMessage field must exist in FlowMessageExt
// with the same number, name and type.
func TestFlowMessageExtMatchesFlowMessage(t *testing.T) {
	core := (&corepb.FlowMessage{}).ProtoReflect().Descriptor().Fields()
	ext := (&FlowMessageExt{}).ProtoReflect().Descriptor().Fields()

	for i := 0; i < core.Len(); i++ {
		want := core.Get(i)
		got := ext.ByNumber(want.Number())
		if got == nil {
			t.Errorf("field %d (%s) missing from FlowMessageExt", want.Number(), want.Name())
			continue
		}
		if got.Name() != want.Name() || got.Kind() != want.Kind() || got.Cardinality() != want.Cardinality() {
			t.Errorf("field %d: FlowMessageExt has %s %s %s, FlowMessage has %s %s %s",
				want.Number(), got.Cardinality(), got.Kind(), got.Name(),
				want.Cardinality(), want.Kind(), want.Name())
		}
	}

	for i := 0; i < ext.Len(); i++ {
		f := ext.Get(i)
		if f.Number() < 1000 && core.ByNumber(f.Number()) == nil {
			t.Errorf("field %d (%s) of FlowMessageExt is not in FlowMessage", f.Number(), f.Name())
		}
	}
}

func TestFlowMessageExtDecodesFlowMessage(t *testing.T) {
	in := &corepb.FlowMessage{
		Type:            corepb.FlowMessage_IPFIX,
		TimeReceivedNs:  1700000000000000001,
		TimeFlowStartNs: 1700000000000000002,
		TimeFlowEndNs:   1700000000000000003,
		SrcAddr:         []byte{10, 0, 0, 1},
		DstAddr:         []byte{10, 0, 0, 2},
		IpFlags:         2,
		TcpFlags:        0x12,
		MplsLabel:       []uint32{16, 17},
		LayerStack:      []corepb.FlowMessage_LayerStack{corepb.FlowMessage_Ethernet, corepb.FlowMessage_IPv4, corepb.FlowMessage_TCP},
	}
	b, err := proto.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}

	var out FlowMessageExt
	if err := proto.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.ProtoReflect().GetUnknown()) != 0 {
		t.Errorf("unknown fields after decoding: %v", out.ProtoReflect().GetUnknown())
	}

	check := func(name string, got, want interface{}) {
		if got != want {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
	check("type", protoreflect.EnumNumber(out.Type), protoreflect.EnumNumber(in.Type))
	check("time_received_ns", out.TimeReceivedNs, in.TimeReceivedNs)
	check("time_flow_start_ns", out.TimeFlowStartNs, in.TimeFlowStartNs)
	check("time_flow_end_ns", out.TimeFlowEndNs, in.TimeFlowEndNs)
	check("ip_flags", out.IpFlags, in.IpFlags)
	check("tcp_flags", out.TcpFlags, in.TcpFlags)
	check("mpls_label", len(out.MplsLabel), len(in.MplsLabel))
	check("layer_stack", len(out.LayerStack), len(in.LayerStack))
}
