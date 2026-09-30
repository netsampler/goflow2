//go:build !reflow_nopcap && cgo

package pcap

import (
	"encoding/binary"
	"net"
	"regexp"
	"testing"

	"github.com/google/gopacket/layers"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func TestTranslateLinuxSLLSynthesizesEthernetPacket(t *testing.T) {
	src := &Source{linkType: uint32(layers.LinkTypeLinuxSLL)}
	inner := []byte{0x45, 0, 0, 20}
	packet := make([]byte, 16+len(inner))
	binary.BigEndian.PutUint16(packet[0:2], 4)
	binary.BigEndian.PutUint16(packet[14:16], 0x0800)
	copy(packet[6:14], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	copy(packet[16:], inner)

	payload, meta := src.translatePacket(packet)
	if meta.headerLength != 16 {
		t.Fatalf("expected SLL header length 16, got %d", meta.headerLength)
	}
	if meta.packetType != "outgoing" {
		t.Fatalf("expected packet type outgoing, got %q", meta.packetType)
	}
	if meta.protocol != 0x0800 {
		t.Fatalf("expected protocol 0x0800, got %#x", meta.protocol)
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
	src := &Source{
		linkType: linkTypeLinuxSLL2,
		interfaceNames: map[uint32]string{
			77: "wan0",
		},
	}
	inner := []byte{0x45, 0, 0, 20}
	packet := make([]byte, 20+len(inner))
	binary.BigEndian.PutUint16(packet[0:2], 0x0800)
	binary.BigEndian.PutUint32(packet[4:8], 77)
	packet[10] = 0
	copy(packet[12:20], []byte{6, 5, 4, 3, 2, 1, 0, 0})
	copy(packet[20:], inner)

	payload, meta := src.translatePacket(packet)
	if meta.headerLength != 20 {
		t.Fatalf("expected SLL2 header length 20, got %d", meta.headerLength)
	}
	if meta.ifIndex != 77 || meta.inputIf != 77 || meta.inputName != "wan0" {
		t.Fatalf("expected input interface metadata for wan0/77, got %#v", meta)
	}
	if meta.outputIf != 0 || meta.outputName != "" {
		t.Fatalf("did not expect output interface metadata, got %#v", meta)
	}
	if got := binary.BigEndian.Uint16(payload[12:14]); got != 0x0800 {
		t.Fatalf("expected synthesized ethertype 0x0800, got %#x", got)
	}
}

func TestDynamicSourceInitEventMarksFirstSeenInterface(t *testing.T) {
	src := &Source{
		cfg: config.SourceConfig{
			Network:     "pcap_live",
			Interface:   "any",
			Type:        "bytes",
			SampleEvery: 1,
		},
		agentIP:               "192.0.2.1",
		captureAny:            true,
		interfaceFilter:       regexp.MustCompile(`^wan`),
		initializedInterfaces: make(map[uint32]struct{}),
	}

	evt := src.dynamicSourceInitEvent(cookedMetadata{ifIndex: 77, ifName: "wan0"}, 3)
	if evt == nil {
		t.Fatalf("expected source_init event")
	}
	if evt.Source.CaptureInterface != "wan0" || evt.Source.CaptureInterfaceIndex != 77 {
		t.Fatalf("unexpected source metadata: %#v", evt.Source)
	}
	if evt.Fields["input_if"] != uint32(77) || evt.Fields["output_if"] != uint32(77) {
		t.Fatalf("unexpected source fields: %#v", evt.Fields)
	}
	if !src.interfaceInitialized(77) {
		t.Fatalf("expected interface 77 to be marked initialized")
	}
}

func TestAnySourceInitEventUsesInterfaceIndexAsDefaultSourceID(t *testing.T) {
	src := &Source{
		cfg: config.SourceConfig{
			Network:     "pcap_live",
			Interface:   "any",
			Type:        "bytes",
			SampleEvery: 1,
		},
		captureAny: true,
	}

	evt := src.sourceInitEvent(net.Interface{Index: 77, Name: "wan0"}, "192.0.2.1", 0)
	if evt.Source.SourceID != 77 || !evt.Source.SourceIDSet {
		t.Fatalf("expected source_id from concrete interface index, got %#v", evt.Source)
	}
	payload, ok := evt.Payload.(event.SourceInit)
	if !ok {
		t.Fatalf("expected SourceInit payload, got %T", evt.Payload)
	}
	if payload.SourceID != 77 {
		t.Fatalf("expected payload source_id=77, got %#v", payload)
	}
}

func TestAnySourceInitEventKeepsExplicitSourceID(t *testing.T) {
	sourceID := uint32(1234)
	src := &Source{
		cfg: config.SourceConfig{
			Network:     "pcap_live",
			Interface:   "any",
			Type:        "bytes",
			SampleEvery: 1,
			SourceID:    &sourceID,
		},
		captureAny: true,
	}

	evt := src.sourceInitEvent(net.Interface{Index: 77, Name: "wan0"}, "192.0.2.1", 0)
	if evt.Source.SourceID != sourceID {
		t.Fatalf("expected explicit source_id=%d, got %#v", sourceID, evt.Source)
	}
	payload := evt.Payload.(event.SourceInit)
	if payload.SourceID != sourceID {
		t.Fatalf("expected payload source_id=%d, got %#v", sourceID, payload)
	}
}
