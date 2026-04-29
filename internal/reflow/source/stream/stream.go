package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcapgo"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type packetReader interface {
	ReadPacketData() ([]byte, gopacket.CaptureInfo, error)
}

type Source struct {
	cfg    config.SourceConfig
	closer io.Closer
}

func New(cfg config.SourceConfig) (*Source, error) {
	if cfg.Network != "stream" {
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
	switch cfg.Type {
	case "pcap", "pcapng", "json":
	default:
		return nil, fmt.Errorf("unsupported stream source type %q", cfg.Type)
	}
	return &Source{cfg: cfg}, nil
}

func (s *Source) InitEvents() ([]*event.Event, error) {
	now := time.Now().UTC()
	name := streamInterfaceName(s.cfg.Address)
	return []*event.Event{
		{
			ReceivedAt: now,
			Kind:       "control",
			Source: event.SourceMetadata{
				Network:          s.cfg.Network,
				Address:          s.cfg.Address,
				Type:             s.cfg.Type,
				CaptureInterface: name,
			},
			Control: &event.ControlMetadata{
				Type:   "source_init",
				Stream: s.cfg.Type,
			},
			Fields: map[string]any{
				"stream_type": s.cfg.Type,
			},
			Payload: event.SourceInit{
				Stream: s.cfg.Type,
			},
		},
	}, nil
}

func (s *Source) Start(ctx context.Context, emit func(*event.Event) error) error {
	r, closer, err := s.open()
	if err != nil {
		return err
	}
	s.closer = closer

	switch s.cfg.Type {
	case "pcap":
		reader, err := pcapgo.NewReader(r)
		if err != nil {
			return fmt.Errorf("open pcap stream: %w", err)
		}
		return s.readPackets(ctx, emit, reader, reader.LinkType())
	case "pcapng":
		reader, err := pcapgo.NewNgReader(r, pcapgo.NgReaderOptions{
			WantMixedLinkType: true,
		})
		if err != nil {
			return fmt.Errorf("open pcapng stream: %w", err)
		}
		return s.readPackets(ctx, emit, reader, 0)
	case "json":
		return s.readNDJSON(ctx, emit, r)
	default:
		return fmt.Errorf("unsupported stream source type %q", s.cfg.Type)
	}
}

func (s *Source) open() (io.Reader, io.Closer, error) {
	if s.cfg.Address == "-" {
		return os.Stdin, os.Stdin, nil
	}
	f, err := os.Open(s.cfg.Address)
	if err != nil {
		return nil, nil, fmt.Errorf("open stream source %s: %w", s.cfg.Address, err)
	}
	return f, f, nil
}

func (s *Source) readPackets(ctx context.Context, emit func(*event.Event) error, reader packetReader, defaultLink layers.LinkType) error {
	for {
		data, ci, err := reader.ReadPacketData()
		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read %s packet: %w", s.cfg.Type, err)
		}

		linkType := defaultLink
		if len(ci.AncillaryData) > 0 {
			if typed, ok := ci.AncillaryData[0].(layers.LinkType); ok {
				linkType = typed
			}
		}
		receivedAt := ci.Timestamp.UTC()
		if receivedAt.IsZero() {
			receivedAt = time.Now().UTC()
		}

		fields := map[string]any{
			"capture_length":      ci.CaptureLength,
			"wire_length":         ci.Length,
			"pcap_link_type":      uint32(linkType),
			"pcap_link_type_name": linkType.String(),
			"stream_type":         s.cfg.Type,
		}
		if protocol := sampledHeaderProtocol(linkType, data); protocol != 0 {
			fields["header_protocol"] = protocol
			fields["protocol"] = protocol
		}

		evt := &event.Event{
			ReceivedAt: receivedAt,
			Source: event.SourceMetadata{
				Network: s.cfg.Network,
				Address: s.cfg.Address,
				Type:    "bytes",
			},
			Payload: append([]byte(nil), data...),
			Fields:  fields,
		}
		if err := emit(evt); err != nil {
			return err
		}
	}
}

func (s *Source) readNDJSON(ctx context.Context, emit func(*event.Event) error, r io.Reader) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line := append([]byte(nil), scanner.Bytes()...)
		if len(line) == 0 {
			continue
		}
		raw := json.RawMessage(line)
		if !json.Valid(raw) {
			return fmt.Errorf("decode ndjson stream: invalid JSON")
		}
		evt := &event.Event{
			ReceivedAt: time.Now().UTC(),
			Source: event.SourceMetadata{
				Network: s.cfg.Network,
				Address: s.cfg.Address,
				Type:    "json",
				JSON: event.JSONMetadata{
					Flavor: s.cfg.JSON.Flavor,
				},
			},
			Message: raw,
		}
		if err := emit(evt); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("read ndjson stream: %w", err)
	}
	return nil
}

func (s *Source) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

func sampledHeaderProtocol(linkType layers.LinkType, data []byte) uint32 {
	switch linkType {
	case layers.LinkTypeEthernet:
		return 1
	case layers.LinkTypeIPv4:
		return 11
	case layers.LinkTypeIPv6:
		return 12
	case layers.LinkTypeRaw:
		if len(data) == 0 {
			return 0
		}
		switch data[0] >> 4 {
		case 4:
			return 11
		case 6:
			return 12
		}
	}
	return 0
}

func streamInterfaceName(address string) string {
	if address == "" || address == "-" {
		return "stdin"
	}
	base := filepath.Base(address)
	if base == "." || base == string(filepath.Separator) {
		return address
	}
	return base
}
