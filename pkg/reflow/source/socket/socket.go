package socket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

type Source struct {
	cfg    config.SourceConfig
	mu     sync.Mutex
	conn   net.PacketConn
	closed bool
}

// New validates the socket-oriented source configuration before runtime start.
func New(cfg config.SourceConfig) (*Source, error) {
	switch cfg.Network {
	case "udp", "unixgram":
	default:
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
	return &Source{cfg: cfg}, nil
}

// InitEvents returns no control events because socket sources do not have
// source-side metadata that needs to be announced before the first packet.
func (s *Source) InitEvents() ([]*event.Event, error) {
	return nil, nil
}

// Start listens for packet-oriented input and turns each datagram into one raw
// source event for the decode stage.
func (s *Source) Start(ctx context.Context, emit func(*event.Event) error) error {
	conn, err := net.ListenPacket(s.cfg.Network, s.cfg.Address)
	if err != nil {
		return fmt.Errorf("listen on %s %s: %w", s.cfg.Network, s.cfg.Address, err)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = conn.Close()
		return nil
	}
	s.conn = conn
	s.mu.Unlock()
	defer func() { _ = s.Close() }()
	stopClose := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stopClose()

	buf := make([]byte, 64*1024)
	for {
		n, remote, err := conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("read datagram: %w", err)
			}
		}
		payload := append([]byte(nil), buf[:n]...)
		evt := &event.Event{
			ReceivedAt: time.Now().UTC(),
			Source: event.SourceMetadata{
				Network: s.cfg.Network,
				Address: s.cfg.Address,
				Type:    s.cfg.Type,
				JSON: event.JSONMetadata{
					Flavor: s.cfg.JSON.Flavor,
				},
			},
		}
		if remote != nil {
			evt.Source.Remote = remote.String()
		}
		switch s.cfg.Type {
		case "flow", "bytes":
			// Binary-oriented source types keep the datagram in Payload so the
			// decode stage can treat it as raw bytes.
			evt.Payload = payload
		default:
			// Message-oriented inputs preserve the raw JSON body when valid and
			// fall back to a quoted string so the event stays representable.
			raw := json.RawMessage(payload)
			if !json.Valid(raw) {
				raw = json.RawMessage([]byte(strconvQuoteBytes(payload)))
			}
			evt.Message = raw
		}
		if err := emit(evt); err != nil {
			return err
		}
	}
}

// Close stops the listener, including when shutdown races with startup.
func (s *Source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// strconvQuoteBytes mirrors strconv.Quote semantics for arbitrary bytes by
// round-tripping through JSON string encoding.
func strconvQuoteBytes(b []byte) string {
	quoted, _ := json.Marshal(string(b))
	return string(quoted)
}
