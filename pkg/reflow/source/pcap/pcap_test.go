//go:build reflow_pcap && cgo && !reflow_dpdk && !reflow_pfring

package pcap

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/google/gopacket/layers"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/processor"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func TestLoopbackCaptureDecodesIPTuple(t *testing.T) {
	ipv4, err := hex.DecodeString("450000281234400040060000c0000201c6336401303901bb00000001000000005002200000000000")
	if err != nil {
		t.Fatal(err)
	}
	ipv6, err := hex.DecodeString("600000000008114020010db800000000000000000000000120010db8000000000000000000000002303901bb00080000")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name     string
		link     layers.LinkType
		family   uint32
		order    binary.ByteOrder
		ip       []byte
		protocol uint32
		src, dst string
	}{
		{"null ipv4 little endian", layers.LinkTypeNull, 2, binary.LittleEndian, ipv4, 11, "192.0.2.1", "198.51.100.1"},
		{"null ipv4 big endian", layers.LinkTypeNull, 2, binary.BigEndian, ipv4, 11, "192.0.2.1", "198.51.100.1"},
		{"null darwin ipv6", layers.LinkTypeNull, 30, binary.LittleEndian, ipv6, 12, "2001:db8::1", "2001:db8::2"},
		{"loop freebsd ipv6", layers.LinkTypeLoop, 28, binary.BigEndian, ipv6, 12, "2001:db8::1", "2001:db8::2"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			frame := make([]byte, 4+len(tt.ip))
			tt.order.PutUint32(frame, tt.family)
			copy(frame[4:], tt.ip)
			normalized := capture.NormalizeFrame(capture.Frame{Data: frame, LinkType: uint32(tt.link)})
			payload := normalized.Data
			evt := &event.Event{
				Source: event.SourceMetadata{Type: "bytes"}, Payload: payload,
				Fields: map[string]any{"capture_length": len(frame), "wire_length": len(frame)},
			}
			for key, value := range normalized.Fields {
				evt.Fields[key] = value
			}
			proc, err := processor.New(config.ProcessorConfig{Type: "builtin"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := proc.Process(evt); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(payload, tt.ip) || evt.Fields["header_protocol"] != tt.protocol || evt.Fields["pcap_link_type"] != uint32(tt.link) {
				t.Fatalf("normalized capture: payload=%x fields=%v", payload, evt.Fields)
			}
			if evt.Fields["src_addr"] != tt.src || evt.Fields["dst_addr"] != tt.dst || evt.Fields["src_port"] != uint32(12345) || evt.Fields["dst_port"] != uint32(443) {
				t.Fatalf("decoded tuple: %v", evt.Fields)
			}
			if evt.Fields["capture_length"] != len(frame) || evt.Fields["wire_length"] != len(frame) || evt.Fields["bytes"] != int64(len(frame)) {
				t.Fatalf("capture lengths changed after stripping header: %v", evt.Fields)
			}
		})
	}
}

func TestTranslateLinuxSLLSynthesizesEthernetPacket(t *testing.T) {
	inner := []byte{0x45, 0, 0, 20}
	packet := make([]byte, 16+len(inner))
	binary.BigEndian.PutUint16(packet[0:2], 4)
	binary.BigEndian.PutUint16(packet[14:16], 0x0800)
	copy(packet[6:14], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	copy(packet[16:], inner)

	meta := capture.NormalizeFrame(capture.Frame{Data: packet, LinkType: 113})
	payload := meta.Data
	if meta.Fields["pcap_cooked_header_length"] != 16 {
		t.Fatalf("expected SLL header length 16, got %d", meta.Fields["pcap_cooked_header_length"])
	}
	if meta.PacketType != "outgoing" {
		t.Fatalf("expected packet type outgoing, got %q", meta.PacketType)
	}
	if meta.Fields["linux_sll_protocol"] != uint32(0x0800) {
		t.Fatalf("expected protocol 0x0800, got %#x", meta.Fields["linux_sll_protocol"])
	}
	if len(payload) != 14+len(inner) {
		t.Fatalf("expected synthesized ethernet length %d, got %d", 14+len(inner), len(payload))
	}
	if got := payload[6:12]; string(got) != string([]byte{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("unexpected synthesized source MAC %v", got)
	}
	if got := binary.BigEndian.Uint16(payload[12:14]); got != 0x0800 {
		t.Fatalf("expected synthesized ethertype 0x0800, got %#x", got)
	}
}

func TestTranslateLinuxSLL2ResolvesInterfaceMetadata(t *testing.T) {
	inner := []byte{0x45, 0, 0, 20}
	packet := make([]byte, 20+len(inner))
	binary.BigEndian.PutUint16(packet[0:2], 0x0800)
	binary.BigEndian.PutUint32(packet[4:8], 77)
	packet[10] = 0
	copy(packet[12:20], []byte{6, 5, 4, 3, 2, 1, 0, 0})
	copy(packet[20:], inner)

	events := fixtureEvents(t, config.SourceConfig{Network: "pcap_live", Type: "bytes", Capture: config.CaptureConfig{Interface: "any"}}, capture.Frame{Data: packet, LinkType: 276, Interface: "wan0"})
	meta := events[len(events)-1]
	payload := meta.Payload.([]byte)
	if meta.Fields["pcap_cooked_header_length"] != 20 {
		t.Fatalf("expected SLL2 header length 20, got %d", meta.Fields["pcap_cooked_header_length"])
	}
	if meta.Source.CaptureInterfaceIndex != 77 || meta.Fields["input_if"] != uint32(77) || meta.Fields["input_interface"] != "wan0" {
		t.Fatalf("expected input interface metadata for wan0/77, got %#v", meta)
	}
	if meta.Fields["output_if"] != nil || meta.Fields["output_interface"] != nil {
		t.Fatalf("did not expect output interface metadata, got %#v", meta)
	}
	if got := binary.BigEndian.Uint16(payload[12:14]); got != 0x0800 {
		t.Fatalf("expected synthesized ethertype 0x0800, got %#x", got)
	}
}

type fixtureDriver struct{ frames []capture.Frame }

func (d *fixtureDriver) Run(_ context.Context, emit func(capture.Frame) error) error {
	for _, frame := range d.frames {
		if err := emit(frame); err != nil {
			return err
		}
	}
	return nil
}
func (*fixtureDriver) Close() error { return nil }

func fixtureEvents(t *testing.T, cfg config.SourceConfig, frames ...capture.Frame) []*event.Event {
	t.Helper()
	src, err := capture.New(cfg, &fixtureDriver{frames: frames})
	if err != nil {
		t.Fatal(err)
	}
	var out []*event.Event
	if err := src.Start(context.Background(), func(evt *event.Event) error { out = append(out, evt); return nil }); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDynamicSourceInitEventMarksFirstSeenInterface(t *testing.T) {
	cfg := config.SourceConfig{Network: "pcap_live", Type: "bytes", Capture: config.CaptureConfig{Interface: "any", InterfaceFilter: "^wan"}}
	frames := []capture.Frame{
		{Data: []byte{1}, LinkType: 1, Interface: "wan0", InterfaceIndex: 77, Drops: 3},
		{Data: []byte{2}, LinkType: 1, Interface: "wan0", InterfaceIndex: 77, Drops: 3},
	}
	events := fixtureEvents(t, cfg, frames...)
	if len(events) != 3 || events[0].Kind != "control" || events[1].Kind == "control" || events[2].Kind == "control" {
		t.Fatalf("expected one initialization and two captures, got %v", events)
	}
	evt := events[0]
	if evt.Source.CaptureInterface != "wan0" || evt.Source.CaptureInterfaceIndex != 77 {
		t.Fatalf("unexpected source metadata: %#v", evt.Source)
	}
	if evt.Fields["input_if"] != uint32(77) || evt.Fields["output_if"] != uint32(77) {
		t.Fatalf("unexpected source fields: %#v", evt.Fields)
	}
	if evt.Source.Sampling.CaptureDropsTotal != 3 || evt.Source.Sampling.Drops != 0 {
		t.Fatalf("lost capture drop count: %#v", evt.Source.Sampling)
	}
}

func TestAnySourceInitEventUsesInterfaceIndexAsDefaultSourceID(t *testing.T) {
	cfg := config.SourceConfig{Network: "pcap_live", Type: "bytes", Capture: config.CaptureConfig{Interface: "any"}}
	events := fixtureEvents(t, cfg, capture.Frame{Data: []byte{1}, LinkType: 1, Interface: "wan0", InterfaceIndex: 77})
	evt := events[0]
	if evt.Source.SourceID != 77 || !evt.Source.SourceIDSet {
		t.Fatalf("expected source_id from concrete interface index, got %#v", evt.Source)
	}
	payload := evt.Payload.(event.SourceInit)
	if payload.SourceID != 77 {
		t.Fatalf("expected payload source_id=77, got %#v", payload)
	}
}

func TestAnySourceInitEventKeepsExplicitSourceID(t *testing.T) {
	sourceID := uint32(1234)
	cfg := config.SourceConfig{Network: "pcap_live", Type: "bytes", SourceID: &sourceID, Capture: config.CaptureConfig{Interface: "any"}}
	events := fixtureEvents(t, cfg, capture.Frame{Data: []byte{1}, LinkType: 1, Interface: "wan0", InterfaceIndex: 77})
	evt := events[0]
	if evt.Source.SourceID != sourceID {
		t.Fatalf("expected explicit source_id=%d, got %#v", sourceID, evt.Source)
	}
	if evt.Payload.(event.SourceInit).SourceID != sourceID {
		t.Fatalf("expected explicit payload source_id=%d", sourceID)
	}
}
