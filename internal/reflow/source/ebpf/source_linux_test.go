//go:build linux

package ebpf

import (
	"testing"

	"golang.org/x/sys/unix"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
)

func TestPacketMetadataMarksOutgoingCapture(t *testing.T) {
	source := &Source{
		cfg:                   config.SourceConfig{Network: "ebpf", Interface: "br-lan", Type: "bytes", SampleEvery: 1},
		agentIP:               "192.0.2.10",
		captureInterfaceIndex: 7,
		seenCount:             1,
	}

	meta := source.packetMetadata(&unix.SockaddrLinklayer{
		Ifindex: 7,
		Pkttype: packetOutgoing,
	})
	evt := source.packetEvent([]byte{0, 1, 2, 3}, meta)

	if evt.Source.CaptureDirection != "out" {
		t.Fatalf("expected source capture direction out, got %q", evt.Source.CaptureDirection)
	}
	if evt.Source.CapturePacketType != "outgoing" {
		t.Fatalf("expected packet type outgoing, got %q", evt.Source.CapturePacketType)
	}
	if got := evt.Fields["output_if"]; got != uint32(7) {
		t.Fatalf("expected output_if=7, got %#v", got)
	}
	if _, ok := evt.Fields["input_if"]; ok {
		t.Fatalf("expected input_if to be unset for outgoing packet, got %#v", evt.Fields["input_if"])
	}
	if got := evt.Fields["output_interface"]; got != "br-lan" {
		t.Fatalf("expected output_interface=br-lan, got %#v", got)
	}
	if got := evt.Fields["dst_interface"]; got != "br-lan" {
		t.Fatalf("expected dst_interface=br-lan, got %#v", got)
	}
}

func TestPacketMetadataMarksIncomingCapture(t *testing.T) {
	source := &Source{
		cfg:                   config.SourceConfig{Network: "ebpf", Interface: "br-lan", Type: "bytes", SampleEvery: 1},
		agentIP:               "192.0.2.10",
		captureInterfaceIndex: 7,
		seenCount:             1,
	}

	meta := source.packetMetadata(&unix.SockaddrLinklayer{
		Ifindex: 7,
		Pkttype: packetHost,
	})
	evt := source.packetEvent([]byte{0, 1, 2, 3}, meta)

	if evt.Source.CaptureDirection != "in" {
		t.Fatalf("expected source capture direction in, got %q", evt.Source.CaptureDirection)
	}
	if evt.Source.CapturePacketType != "host" {
		t.Fatalf("expected packet type host, got %q", evt.Source.CapturePacketType)
	}
	if got := evt.Fields["input_if"]; got != uint32(7) {
		t.Fatalf("expected input_if=7, got %#v", got)
	}
	if _, ok := evt.Fields["output_if"]; ok {
		t.Fatalf("expected output_if to be unset for incoming packet, got %#v", evt.Fields["output_if"])
	}
	if got := evt.Fields["input_interface"]; got != "br-lan" {
		t.Fatalf("expected input_interface=br-lan, got %#v", got)
	}
	if got := evt.Fields["src_interface"]; got != "br-lan" {
		t.Fatalf("expected src_interface=br-lan, got %#v", got)
	}
}
