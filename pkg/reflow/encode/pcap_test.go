package encode

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func TestPcapEncoderWritesHeaderOnceAndPackets(t *testing.T) {
	enc, err := NewPcapEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "auto",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapEncoder returned error: %v", err)
	}

	first := &event.Event{
		ReceivedAt: time.Unix(10, 123).UTC(),
		Fields: map[string]any{
			"header_data":     ethernetFrame(0x0800),
			"protocol":        uint32(1),
			"original_length": uint32(128),
		},
	}
	second := &event.Event{
		ReceivedAt: time.Unix(11, 456).UTC(),
		Fields: map[string]any{
			"header_data":  ethernetFrame(0x86dd),
			"protocol":     uint32(1),
			"frame_length": uint32(96),
		},
	}

	payloads, err := enc.Encode(first)
	if err != nil {
		t.Fatalf("Encode first returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one payload, got %d", len(payloads))
	}
	payloads2, err := enc.Encode(second)
	if err != nil {
		t.Fatalf("Encode second returned error: %v", err)
	}
	combined := append(append([]byte(nil), payloads[0]...), payloads2[0]...)

	reader, err := pcapgo.NewReader(bytes.NewReader(combined))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	if reader.LinkType() != layers.LinkTypeEthernet {
		t.Fatalf("expected ethernet link type, got %v", reader.LinkType())
	}
	data, ci, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData first returned error: %v", err)
	}
	if len(data) != len(first.Fields["header_data"].([]byte)) {
		t.Fatalf("expected first caplen %d, got %d", len(first.Fields["header_data"].([]byte)), len(data))
	}
	if ci.Length != 128 {
		t.Fatalf("expected first wire length 128, got %d", ci.Length)
	}
	_, ci, err = reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData second returned error: %v", err)
	}
	if ci.Length != 96 {
		t.Fatalf("expected second wire length 96, got %d", ci.Length)
	}
	if bytes.Count(combined, combined[:24]) != 1 {
		t.Fatalf("expected pcap global header to appear once")
	}
}

func TestPcapEncoderUsesPseudoPacket(t *testing.T) {
	enc, err := NewPcapEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "pseudo",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapEncoder returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: time.Unix(20, 0).UTC(),
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
	reader, err := pcapgo.NewReader(bytes.NewReader(payloads[0]))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	data, _, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData returned error: %v", err)
	}
	if len(data) < 14 || data[12] != 0x08 || data[13] != 0x00 {
		t.Fatalf("expected synthetic ethernet IPv4 frame, got %x", data[:min(len(data), 14)])
	}
}

func TestPcapEncoderUsesAverageAggregateLengthForPseudoPacket(t *testing.T) {
	enc, err := NewPcapEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "auto",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapEncoder returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(17),
			"src_port": uint32(12345),
			"dst_port": uint32(53),
			"bytes":    uint64(3000),
			"packets":  uint64(2),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	reader, err := pcapgo.NewReader(bytes.NewReader(payloads[0]))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	data, ci, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData returned error: %v", err)
	}
	if len(data) >= ci.Length {
		t.Fatalf("expected pseudo packet to be captured/truncated, caplen=%d wirelen=%d", len(data), ci.Length)
	}
	if ci.Length != 1500 {
		t.Fatalf("expected average aggregate wire length 1500, got %d", ci.Length)
	}
}

func TestPcapEncoderUsesHeaderHexWhenHeaderDataMissing(t *testing.T) {
	enc, err := NewPcapEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "auto",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapEncoder returned error: %v", err)
	}
	frame := ethernetFrame(0x0800)

	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: time.Unix(20, 0).UTC(),
		Fields: map[string]any{
			"header_hex":      " " + hex.EncodeToString(frame[:10]) + ":" + hex.EncodeToString(frame[10:]),
			"protocol":        uint32(1),
			"original_length": uint32(len(frame)),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	reader, err := pcapgo.NewReader(bytes.NewReader(payloads[0]))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	data, _, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData returned error: %v", err)
	}
	if !bytes.Equal(data, frame) {
		t.Fatalf("expected decoded header_hex frame, got %x", data)
	}
}

func TestPcapEncoderKeepsPseudoEthernetStartLayer(t *testing.T) {
	enc, err := NewPcapEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "pseudo",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapEncoder returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"src_mac":  "00:11:22:33:44:55",
			"dst_mac":  "66:77:88:99:aa:bb",
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(17),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	reader, err := pcapgo.NewReader(bytes.NewReader(payloads[0]))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	data, _, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData returned error: %v", err)
	}
	if len(data) < 14 || data[0] != 0x66 || data[6] != 0x00 || data[12] != 0x08 || data[13] != 0x00 {
		t.Fatalf("expected original pseudo ethernet frame, got %x", data[:min(len(data), 14)])
	}
}

func TestPcapEncoderPseudoStartLayerIPStripsEthernet(t *testing.T) {
	enc, err := NewPcapEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "pseudo",
			LinkType:     "raw",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapEncoder returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		Fields: map[string]any{
			"src_mac":  "00:11:22:33:44:55",
			"dst_mac":  "66:77:88:99:aa:bb",
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(17),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	reader, err := pcapgo.NewReader(bytes.NewReader(payloads[0]))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	data, _, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData returned error: %v", err)
	}
	if len(data) == 0 || data[0]>>4 != 4 {
		t.Fatalf("expected raw IPv4 packet, got %x", data[:min(len(data), 14)])
	}
}

func TestPcapEncoderHeaderDataFallsBackToPseudoPacket(t *testing.T) {
	enc, err := NewPcapEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "header_data",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapEncoder returned error: %v", err)
	}

	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: time.Unix(21, 0).UTC(),
		Fields: map[string]any{
			"record_kind": "packet",
			"src_addr":    "192.0.2.10",
			"dst_addr":    "198.51.100.20",
			"proto":       uint32(6),
			"src_port":    uint32(54321),
			"dst_port":    uint32(443),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	reader, err := pcapgo.NewReader(bytes.NewReader(payloads[0]))
	if err != nil {
		t.Fatalf("NewReader returned error: %v", err)
	}
	data, _, err := reader.ReadPacketData()
	if err != nil {
		t.Fatalf("ReadPacketData returned error: %v", err)
	}
	if len(data) < 14 || data[12] != 0x08 || data[13] != 0x00 {
		t.Fatalf("expected pseudo ethernet IPv4 frame, got %x", data[:min(len(data), 14)])
	}
}

func TestPcapEncoderRejectsIncompatibleLinkType(t *testing.T) {
	enc, err := NewPcapEncoder(config.EncoderConfig{
		Pcap: config.PcapConfig{
			PacketSource: "header_data",
			LinkType:     "ethernet",
			SnapLen:      65535,
		},
	})
	if err != nil {
		t.Fatalf("NewPcapEncoder returned error: %v", err)
	}
	_, err = enc.Encode(&event.Event{
		Fields: map[string]any{
			"header_data": []byte{0x45, 0, 0, 20},
			"protocol":    uint32(11),
		},
	})
	if err == nil {
		t.Fatalf("expected incompatible link type error")
	}
}

func ethernetFrame(etherType uint16) []byte {
	data := make([]byte, 34)
	data[12] = byte(etherType >> 8)
	data[13] = byte(etherType)
	if etherType == 0x0800 {
		data[14] = 0x45
		data[23] = 17
	}
	if etherType == 0x86dd {
		data[14] = 0x60
		data[20] = 17
	}
	return data
}
