package socket

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type Source struct {
	cfg  config.SourceConfig
	conn net.PacketConn
	wg   sync.WaitGroup
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
	s.conn = conn

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		_ = s.conn.Close()
	}()

	buf := make([]byte, 64*1024)
	for {
		n, remote, err := s.conn.ReadFrom(buf)
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

// Close stops the listener and waits for the background context closer to exit.
func (s *Source) Close() error {
	if s.conn != nil {
		err := s.conn.Close()
		s.wg.Wait()
		return err
	}
	s.wg.Wait()
	return nil
}

// strconvQuoteBytes mirrors strconv.Quote semantics for arbitrary bytes by
// round-tripping through JSON string encoding.
func strconvQuoteBytes(b []byte) string {
	quoted, _ := json.Marshal(string(b))
	return string(quoted)
}
