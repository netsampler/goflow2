package sink

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
)

type Sink interface {
	Send(payload []byte) error
	Close() error
}

// New selects the concrete output sink from the normalized sink config.
func New(cfg config.SinkConfig) (Sink, error) {
	switch cfg.Type {
	case "", "stdout":
		return &writerSink{w: os.Stdout, sep: []byte("\n")}, nil
	case "file":
		f, err := os.OpenFile(cfg.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("open sink file %s: %w", cfg.Path, err)
		}
		return &writerSink{w: f, f: f, sep: []byte("\n")}, nil
	case "udp", "unixgram":
		conn, err := net.Dial(cfg.Type, cfg.Address)
		if err != nil {
			return nil, fmt.Errorf("dial sink %s %s: %w", cfg.Type, cfg.Address, err)
		}
		return &packetSink{conn: conn}, nil
	default:
		return nil, fmt.Errorf("unsupported sink.type %q", cfg.Type)
	}
}

type writerSink struct {
	mu  sync.Mutex
	w   io.Writer
	f   *os.File
	sep []byte
}

// Send writes one payload and appends the configured record separator.
func (s *writerSink) Send(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(payload); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}
	if len(s.sep) > 0 {
		if _, err := s.w.Write(s.sep); err != nil {
			return fmt.Errorf("write separator: %w", err)
		}
	}
	return nil
}

// Close only needs to close file-backed sinks; stdout remains owned by the process.
func (s *writerSink) Close() error {
	if s.f != nil {
		return s.f.Close()
	}
	return nil
}

type packetSink struct {
	mu   sync.Mutex
	conn net.Conn
}

// Send writes one already-encoded packet to the connected datagram sink.
func (s *packetSink) Send(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.conn.Write(payload); err != nil {
		return fmt.Errorf("write packet: %w", err)
	}
	return nil
}

// Close releases the connected socket.
func (s *packetSink) Close() error {
	return s.conn.Close()
}
