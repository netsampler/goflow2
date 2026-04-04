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

func New(cfg config.SourceConfig) (*Source, error) {
	switch cfg.Network {
	case "udp", "unixgram":
	default:
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
	return &Source{cfg: cfg}, nil
}

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
		raw := json.RawMessage(payload)
		if !json.Valid(raw) {
			raw = json.RawMessage([]byte(strconvQuoteBytes(payload)))
		}

		evt := &event.Event{
			ReceivedAt: time.Now().UTC(),
			Source: event.SourceMetadata{
				Network:     s.cfg.Network,
				Address:     s.cfg.Address,
				Frame:       s.cfg.Frame,
				MessageType: s.cfg.MessageType,
			},
			Message: raw,
		}
		if remote != nil {
			evt.Source.Remote = remote.String()
		}
		if err := emit(evt); err != nil {
			return err
		}
	}
}

func (s *Source) Close() error {
	if s.conn != nil {
		err := s.conn.Close()
		s.wg.Wait()
		return err
	}
	s.wg.Wait()
	return nil
}

func strconvQuoteBytes(b []byte) string {
	quoted, _ := json.Marshal(string(b))
	return string(quoted)
}
