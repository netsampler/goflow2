package encode

import (
	"bytes"
	"fmt"
	"path/filepath"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type PcapNGEncoder struct {
	packet        *PcapEncoder
	buf           bytes.Buffer
	writer        *pcapgo.NgWriter
	headerWritten bool
	iface         pcapNGInterfaceMetadata
}

func NewPcapNGEncoder(cfg config.EncoderConfig) (*PcapNGEncoder, error) {
	packet, err := NewPcapEncoder(cfg)
	if err != nil {
		return nil, err
	}
	return &PcapNGEncoder{packet: packet}, nil
}

func (e *PcapNGEncoder) Encode(evt *event.Event) ([][]byte, error) {
	if evt == nil {
		return nil, nil
	}
	if evt.Kind == "control" {
		e.handleControl(evt)
		return nil, nil
	}

	data, source, err := e.packet.packetBytes(evt)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("encode pcapng: empty packet bytes")
	}
	data = e.packet.adaptPacketBytes(evt, data, source)
	if err := e.packet.validateLinkType(evt, data, source); err != nil {
		return nil, err
	}

	if !e.headerWritten {
		iface := e.interfaceBlock()
		writer, err := pcapgo.NewNgWriterInterface(&e.buf, pcapgo.NgInterface{
			Name:        iface.Name,
			Description: iface.Description,
			Comment:     iface.Comment,
			LinkType:    e.packet.linkType,
			SnapLength:  uint32(e.packet.cfg.SnapLen),
		}, pcapgo.NgWriterOptions{
			SectionInfo: pcapgo.NgSectionInfo{
				Application: "reflow",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("write pcapng header: %w", err)
		}
		e.writer = writer
		e.headerWritten = true
	}

	ci := gopacket.CaptureInfo{
		Timestamp:      pcapTimestamp(evt),
		CaptureLength:  len(data),
		Length:         pcapWireLength(evt.Fields, len(data)),
		InterfaceIndex: 0,
	}
	if err := e.writer.WritePacket(ci, data); err != nil {
		return nil, fmt.Errorf("write pcapng packet: %w", err)
	}
	if err := e.writer.Flush(); err != nil {
		return nil, fmt.Errorf("flush pcapng packet: %w", err)
	}

	out := append([]byte(nil), e.buf.Bytes()...)
	e.buf.Reset()
	return [][]byte{out}, nil
}

func (e *PcapNGEncoder) handleControl(evt *event.Event) {
	if evt == nil || evt.Control == nil || evt.Control.Type != "source_init" || e.headerWritten {
		return
	}
	if name := pcapNGInterfaceNameFromSource(evt.Source); name != "" {
		e.iface.Name = name
	}
	if e.iface.Description == "" {
		e.iface.Description = pcapNGInterfaceDescription(evt.Source)
	}
}

func (e *PcapNGEncoder) Flush() ([][]byte, error) {
	if e.writer == nil {
		return nil, nil
	}
	if err := e.writer.Flush(); err != nil {
		return nil, fmt.Errorf("flush pcapng encoder: %w", err)
	}
	if e.buf.Len() == 0 {
		return nil, nil
	}
	out := append([]byte(nil), e.buf.Bytes()...)
	e.buf.Reset()
	return [][]byte{out}, nil
}

func pcapNGInterfaceName(linkType layers.LinkType) string {
	switch linkType {
	case layers.LinkTypeEthernet:
		return "reflow-ethernet"
	case layers.LinkTypeRaw, layers.LinkTypeIPv4, layers.LinkTypeIPv6:
		return "reflow-raw"
	default:
		return "reflow"
	}
}

type pcapNGInterfaceMetadata struct {
	Name        string
	Description string
	Comment     string
}

func (e *PcapNGEncoder) interfaceBlock() pcapNGInterfaceMetadata {
	iface := e.iface
	if iface.Name == "" {
		iface.Name = pcapNGInterfaceName(e.packet.linkType)
	}
	return iface
}

func pcapNGInterfaceNameFromSource(src event.SourceMetadata) string {
	if src.CaptureInterface != "" {
		return src.CaptureInterface
	}
	if src.Address == "" {
		return ""
	}
	if src.Address == "-" {
		return "stdin"
	}
	base := filepath.Base(src.Address)
	if base == "." || base == string(filepath.Separator) {
		return src.Address
	}
	return base
}

func pcapNGInterfaceDescription(src event.SourceMetadata) string {
	if src.Network == "" && src.Type == "" && src.Address == "" {
		return ""
	}
	return fmt.Sprintf("network=%s type=%s address=%s", src.Network, src.Type, src.Address)
}
