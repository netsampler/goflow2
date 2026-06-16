package sink

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

type Sink interface {
	Send(payload []byte) error
	Close() error
}

// New selects the concrete output sink from the normalized sink config.
func New(cfg config.SinkConfig) (Sink, error) {
	sep := []byte("\n")
	if cfg.Framing == "none" {
		sep = nil
	}
	switch cfg.Type {
	case "", "stdout":
		return &writerSink{w: os.Stdout, sep: sep}, nil
	case "file":
		flags := os.O_APPEND | os.O_CREATE | os.O_WRONLY
		if cfg.Mode == "truncate" {
			flags = os.O_CREATE | os.O_TRUNC | os.O_WRONLY
		}
		f, err := os.OpenFile(cfg.Path, flags, 0644)
		if err != nil {
			return nil, fmt.Errorf("open sink file %s: %w", cfg.Path, err)
		}
		return &writerSink{w: f, f: f, sep: sep}, nil
	case "udp", "unixgram":
		resolveInterval := time.Duration(0)
		if cfg.ResolveIntervalMS != nil {
			resolveInterval = time.Duration(*cfg.ResolveIntervalMS) * time.Millisecond
		}
		sink, err := newPacketSink(cfg.Type, cfg.Address, resolveInterval, net.Dial, time.Now)
		if err != nil {
			return nil, fmt.Errorf("dial sink %s %s: %w", cfg.Type, cfg.Address, err)
		}
		return sink, nil
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
	mu              sync.Mutex
	network         string
	address         string
	resolveInterval time.Duration
	lastResolve     time.Time
	conn            net.Conn
	dial            func(network, address string) (net.Conn, error)
	now             func() time.Time
}

func newPacketSink(network, address string, resolveInterval time.Duration, dial func(string, string) (net.Conn, error), now func() time.Time) (*packetSink, error) {
	if dial == nil {
		dial = net.Dial
	}
	if now == nil {
		now = time.Now
	}
	conn, err := dial(network, address)
	if err != nil {
		return nil, err
	}
	return &packetSink{
		network:         network,
		address:         address,
		resolveInterval: resolveInterval,
		lastResolve:     now(),
		conn:            conn,
		dial:            dial,
		now:             now,
	}, nil
}

// Send writes one already-encoded packet to the connected datagram sink.
func (s *packetSink) Send(payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shouldRefreshLocked() {
		if err := s.refreshLocked(); err != nil {
			// Keep exporting through the previous socket if DNS is temporarily unavailable.
			s.lastResolve = s.now()
		}
	}
	if _, err := s.conn.Write(payload); err != nil {
		if s.resolveInterval > 0 {
			if refreshErr := s.refreshLocked(); refreshErr != nil {
				return fmt.Errorf("write packet: %w; refresh packet sink: %v", err, refreshErr)
			}
			if _, retryErr := s.conn.Write(payload); retryErr != nil {
				return fmt.Errorf("write packet after refresh: %w", retryErr)
			}
			return nil
		}
		return fmt.Errorf("write packet: %w", err)
	}
	return nil
}

func (s *packetSink) shouldRefreshLocked() bool {
	if s.resolveInterval <= 0 {
		return false
	}
	return !s.now().Before(s.lastResolve.Add(s.resolveInterval))
}

func (s *packetSink) refreshLocked() error {
	conn, err := s.dial(s.network, s.address)
	if err != nil {
		return err
	}
	old := s.conn
	s.conn = conn
	s.lastResolve = s.now()
	if old != nil {
		_ = old.Close()
	}
	return nil
}

// Close releases the connected socket.
func (s *packetSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conn.Close()
}
