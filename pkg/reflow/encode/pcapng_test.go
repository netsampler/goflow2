package encode

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func TestPcapNGEncoderWritesReadablePacket(t *testing.T) {
	enc, err := NewPcapNGEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "auto",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapNGEncoder returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: time.Unix(30, 123).UTC(),
		Fields: map[string]any{
			"header_data":     ethernetFrame(0x0800),
			"protocol":        uint32(1),
			"original_length": uint32(128),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one payload, got %d", len(payloads))
	}

	reader, err := pcapgo.NewNgReader(bytes.NewReader(payloads[0]), pcapgo.NgReaderOptions{})
	if err != nil {
		t.Fatalf("NewNgReader returned error: %v", err)
	}
	if reader.LinkType() != layers.LinkTypeEthernet {
		t.Fatalf("expected ethernet link type, got %v", reader.LinkType())
	}
	data, ci, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData returned error: %v", err)
	}
	if len(data) != len(ethernetFrame(0x0800)) {
		t.Fatalf("expected packet bytes, got caplen %d", len(data))
	}
	if ci.Length != 128 {
		t.Fatalf("expected wire length 128, got %d", ci.Length)
	}
}

func TestPcapNGEncoderUsesSourceInitInterfaceName(t *testing.T) {
	enc, err := NewPcapNGEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "auto",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapNGEncoder returned error: %v", err)
	}

	if payloads, err := enc.Encode(&event.Event{
		Kind: "control",
		Source: event.SourceMetadata{
			Network:          "pcap_live",
			Address:          "en7",
			Type:             "bytes",
			CaptureInterface: "en7",
		},
		Control: &event.ControlMetadata{
			Type: "source_init",
		},
	}); err != nil || len(payloads) != 0 {
		t.Fatalf("expected source_init to be consumed without payloads, payloads=%d err=%v", len(payloads), err)
	}

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"header_data": ethernetFrame(0x0800),
			"protocol":    uint32(1),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	reader, err := pcapgo.NewNgReader(bytes.NewReader(payloads[0]), pcapgo.NgReaderOptions{})
	if err != nil {
		t.Fatalf("NewNgReader returned error: %v", err)
	}
	iface, err := reader.Interface(0)
	if err != nil {
		t.Fatalf("Interface returned error: %v", err)
	}
	if iface.Name != "en7" {
		t.Fatalf("expected interface name en7, got %q", iface.Name)
	}
}

func TestPcapNGEncoderUsesPseudoRawPacket(t *testing.T) {
	enc, err := NewPcapNGEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "pseudo",
			LinkType:     "raw",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapNGEncoder returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(17),
			"src_port": uint32(12345),
			"dst_port": uint32(53),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	reader, err := pcapgo.NewNgReader(bytes.NewReader(payloads[0]), pcapgo.NgReaderOptions{})
	if err != nil {
		t.Fatalf("NewNgReader returned error: %v", err)
	}
	if reader.LinkType() != layers.LinkTypeRaw {
		t.Fatalf("expected raw link type, got %v", reader.LinkType())
	}
	data, _, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData returned error: %v", err)
	}
	if len(data) == 0 || data[0]>>4 != 4 {
		t.Fatalf("expected raw IPv4 packet, got %x", data[:min(len(data), 14)])
	}
}
