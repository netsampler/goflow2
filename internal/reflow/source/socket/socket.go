package socket

import (
	"context"
	"encoding/binary"
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
		case "flow":
			flowType, flowVersion, err := identifyFlow(payload)
			if err != nil {
				return err
			}
			evt.Payload = payload
			evt.Fields = map[string]any{
				"message_type": "flow",
				"flow_type":    flowType,
				"flow_version": flowVersion,
			}
		case "bytes":
			evt.Payload = payload
			evt.Fields = map[string]any{
				"message_type": "bytes",
			}
		default:
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

func identifyFlow(payload []byte) (string, uint32, error) {
	if len(payload) < 4 {
		return "", 0, fmt.Errorf("identify flow: payload too short")
	}
	if binary.BigEndian.Uint32(payload[:4]) == 5 {
		return "sflow", 5, nil
	}
	switch version := binary.BigEndian.Uint16(payload[:2]); version {
	case 5:
		return "netflowv5", uint32(version), nil
	case 9:
		return "netflowv9", uint32(version), nil
	case 10:
		return "ipfix", uint32(version), nil
	default:
		return "", 0, fmt.Errorf("identify flow: unsupported version %d", version)
	}
}
