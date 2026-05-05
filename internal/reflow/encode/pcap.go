package encode

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/packet"
)

type PcapEncoder struct {
	cfg           config.PcapConfig
	linkType      layers.LinkType
	buf           bytes.Buffer
	writer        *pcapgo.Writer
	headerWritten bool
}

func NewPcapEncoder(cfg config.EncoderConfig) (*PcapEncoder, error) {
	linkType, err := pcapLinkType(cfg.Pcap.LinkType)
	if err != nil {
		return nil, err
	}
	return &PcapEncoder{
		cfg:      cfg.Pcap,
		linkType: linkType,
	}, nil
}

func (e *PcapEncoder) Encode(evt *event.Event) ([][]byte, error) {
	if evt == nil || evt.Kind == "control" {
		return nil, nil
	}

	data, source, err := e.packetBytes(evt)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("encode pcap: empty packet bytes")
	}
	data = e.adaptPacketBytes(evt, data, source)
	if err := e.validateLinkType(evt, data, source); err != nil {
		return nil, err
	}

	if !e.headerWritten {
		e.writer = pcapgo.NewWriterNanos(&e.buf)
		if err := e.writer.WriteFileHeader(uint32(e.cfg.SnapLen), e.linkType); err != nil {
			return nil, fmt.Errorf("write pcap header: %w", err)
		}
		e.headerWritten = true
	}

	ci := gopacket.CaptureInfo{
		Timestamp:     pcapTimestamp(evt),
		CaptureLength: len(data),
		Length:        pcapWireLength(evt.Fields, len(data)),
	}
	if err := e.writer.WritePacket(ci, data); err != nil {
		return nil, fmt.Errorf("write pcap packet: %w", err)
	}

	out := append([]byte(nil), e.buf.Bytes()...)
	e.buf.Reset()
	return [][]byte{out}, nil
}

func (e *PcapEncoder) Flush() ([][]byte, error) {
	return nil, nil
}

func (e *PcapEncoder) packetBytes(evt *event.Event) ([]byte, string, error) {
	switch e.cfg.PacketSource {
	case "header_data":
		if data := packetHeaderBytes(evt.Fields); len(data) > 0 {
			return data, "header_data", nil
		}
		if data, ok := packet.BuildPseudoHeader(evt, evt.Fields); ok && len(data) > 0 {
			return data, "pseudo", nil
		}
		return nil, "", fmt.Errorf("encode pcap: missing header_data and cannot build pseudo packet")
	case "payload":
		data, ok := evt.Payload.([]byte)
		if !ok || len(data) == 0 {
			return nil, "", fmt.Errorf("encode pcap: missing payload bytes")
		}
		return append([]byte(nil), data...), "payload", nil
	case "pseudo":
		data, ok := packet.BuildPseudoHeader(evt, evt.Fields)
		if !ok || len(data) == 0 {
			return nil, "", fmt.Errorf("encode pcap: cannot build pseudo packet")
		}
		return data, "pseudo", nil
	case "auto":
		if data := packetHeaderBytes(evt.Fields); len(data) > 0 {
			return data, "header_data", nil
		}
		if data, ok := evt.Payload.([]byte); ok && len(data) > 0 {
			return append([]byte(nil), data...), "payload", nil
		}
		if data, ok := packet.BuildPseudoHeader(evt, evt.Fields); ok && len(data) > 0 {
			return data, "pseudo", nil
		}
		return nil, "", fmt.Errorf("encode pcap: missing packet bytes")
	default:
		return nil, "", fmt.Errorf("unsupported pcap packet_source %q", e.cfg.PacketSource)
	}
}

func packetHeaderBytes(fields map[string]any) []byte {
	if data := bytesField(fields, "header_data"); len(data) > 0 {
		return append([]byte(nil), data...)
	}
	raw := stringFieldOrZero(fields, "header_hex")
	if raw == "" {
		return nil
	}
	raw = strings.ReplaceAll(raw, " ", "")
	raw = strings.ReplaceAll(raw, ":", "")
	data, err := hex.DecodeString(raw)
	if err != nil || len(data) == 0 {
		return nil
	}
	return data
}

func (e *PcapEncoder) adaptPacketBytes(evt *event.Event, data []byte, source string) []byte {
	if source != "pseudo" || len(data) == 0 {
		return data
	}
	startLayer := pseudoPacketStartLayer(evt, data)
	if e.linkType == layers.LinkTypeEthernet && startLayer == "ip" {
		switch data[0] >> 4 {
		case 4:
			return prependSyntheticEthernet(data, 0x0800)
		case 6:
			return prependSyntheticEthernet(data, 0x86dd)
		}
	}
	switch e.linkType {
	case layers.LinkTypeRaw, layers.LinkTypeIPv4, layers.LinkTypeIPv6:
		if startLayer == "ethernet" && len(data) >= 14 {
			etherType := uint16(data[12])<<8 | uint16(data[13])
			switch etherType {
			case 0x0800, 0x86dd:
				return append([]byte(nil), data[14:]...)
			}
		}
	}
	return data
}

func (e *PcapEncoder) validateLinkType(evt *event.Event, data []byte, source string) error {
	protocol := uint32Field(evt.Fields, "header_protocol")
	if protocol == 0 {
		protocol = uint32Field(evt.Fields, "protocol")
	}
	if source == "pseudo" {
		protocol = sampledProtocolFromBytes(data)
	}
	switch e.linkType {
	case layers.LinkTypeEthernet:
		if protocol != 0 && protocol != 1 {
			return fmt.Errorf("encode pcap: packet protocol %d is incompatible with link_type ethernet", protocol)
		}
	case layers.LinkTypeIPv4:
		if protocol != 0 && protocol != 11 {
			return fmt.Errorf("encode pcap: packet protocol %d is incompatible with link_type ipv4", protocol)
		}
	case layers.LinkTypeIPv6:
		if protocol != 0 && protocol != 12 {
			return fmt.Errorf("encode pcap: packet protocol %d is incompatible with link_type ipv6", protocol)
		}
	case layers.LinkTypeRaw:
		if protocol != 0 && protocol != 11 && protocol != 12 {
			return fmt.Errorf("encode pcap: packet protocol %d is incompatible with link_type raw", protocol)
		}
	}
	return nil
}

func pcapTimestamp(evt *event.Event) time.Time {
	if evt != nil && !evt.ReceivedAt.IsZero() {
		return evt.ReceivedAt.UTC()
	}
	return time.Now().UTC()
}

func pcapWireLength(fields map[string]any, captured int) int {
	for _, key := range []string{"original_length", "frame_length", "wire_length"} {
		if value := uint32Field(fields, key); value > 0 {
			return int(value)
		}
	}
	return captured
}

func pcapLinkType(name string) (layers.LinkType, error) {
	switch name {
	case "", "ethernet":
		return layers.LinkTypeEthernet, nil
	case "raw":
		return layers.LinkTypeRaw, nil
	case "ipv4":
		return layers.LinkTypeIPv4, nil
	case "ipv6":
		return layers.LinkTypeIPv6, nil
	default:
		return 0, fmt.Errorf("unsupported pcap link_type %q", name)
	}
}

func sampledProtocolFromBytes(data []byte) uint32 {
	if len(data) >= 14 {
		etherType := uint16(data[12])<<8 | uint16(data[13])
		switch etherType {
		case 0x0800, 0x86dd:
			return 1
		}
	}
	if len(data) > 0 {
		switch data[0] >> 4 {
		case 4:
			return 11
		case 6:
			return 12
		}
	}
	return 0
}

func pseudoPacketStartLayer(evt *event.Event, data []byte) string {
	if evt != nil && evt.Packet != nil && len(evt.Packet.Layers) > 0 {
		switch evt.Packet.Layers[0].Kind {
		case "ethernet", "dot1q", "mpls", "pppoe":
			return "ethernet"
		case "ipv4", "ipv6":
			return "ip"
		}
	}
	switch sampledProtocolFromBytes(data) {
	case 1:
		return "ethernet"
	case 11, 12:
		return "ip"
	default:
		return ""
	}
}

func prependSyntheticEthernet(payload []byte, etherType uint16) []byte {
	// When pcap is configured as Ethernet but the pseudo packet starts at IP,
	// wrap it with a minimal Ethernet header. The zero-value bytes are unknown
	// destination/source MACs; only EtherType is meaningful.
	out := make([]byte, 14+len(payload))
	out[12] = byte(etherType >> 8)
	out[13] = byte(etherType)
	copy(out[14:], payload)
	return out
}
