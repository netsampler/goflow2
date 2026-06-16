package sink

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

func TestFileSinkFramingNoneAndTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.bin")
	if err := os.WriteFile(path, []byte("old data"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	s, err := New(config.SinkConfig{
		Type:    "file",
		Path:    path,
		Framing: "none",
		Mode:    "truncate",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if err := s.Send([]byte{0, 1, 2}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != string([]byte{0, 1, 2}) {
		t.Fatalf("expected exact binary payload, got %v", got)
	}
}

func TestPacketSinkRefreshesConnectionAfterInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	var conns []*packetTestConn
	s, err := newPacketSink("udp", "collector.example.com:4739", time.Second, func(network, address string) (net.Conn, error) {
		if network != "udp" || address != "collector.example.com:4739" {
			t.Fatalf("unexpected dial target %s %s", network, address)
		}
		conn := &packetTestConn{}
		conns = append(conns, conn)
		return conn, nil
	}, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("newPacketSink returned error: %v", err)
	}

	if err := s.Send([]byte("first")); err != nil {
		t.Fatalf("first Send returned error: %v", err)
	}
	now = now.Add(time.Second)
	if err := s.Send([]byte("second")); err != nil {
		t.Fatalf("second Send returned error: %v", err)
	}

	if len(conns) != 2 {
		t.Fatalf("expected two dials, got %d", len(conns))
	}
	if !conns[0].closed {
		t.Fatalf("expected old connection to be closed after refresh")
	}
	if string(conns[0].writes[0]) != "first" || string(conns[1].writes[0]) != "second" {
		t.Fatalf("unexpected writes: first=%q second=%q", conns[0].writes, conns[1].writes)
	}
}

func TestPacketSinkResolveIntervalZeroDisablesRefresh(t *testing.T) {
	now := time.Unix(1000, 0)
	var conns []*packetTestConn
	s, err := newPacketSink("udp", "collector.example.com:4739", 0, func(string, string) (net.Conn, error) {
		conn := &packetTestConn{}
		conns = append(conns, conn)
		return conn, nil
	}, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("newPacketSink returned error: %v", err)
	}

	if err := s.Send([]byte("first")); err != nil {
		t.Fatalf("first Send returned error: %v", err)
	}
	now = now.Add(24 * time.Hour)
	if err := s.Send([]byte("second")); err != nil {
		t.Fatalf("second Send returned error: %v", err)
	}

	if len(conns) != 1 {
		t.Fatalf("expected one dial when refresh is disabled, got %d", len(conns))
	}
	if len(conns[0].writes) != 2 {
		t.Fatalf("expected both writes on initial connection, got %d", len(conns[0].writes))
	}
}

func TestPacketSinkUsesExistingConnectionWhenScheduledRefreshFails(t *testing.T) {
	resolveErr := errors.New("resolve failed")
	now := time.Unix(1000, 0)
	var dialAttempts int
	var conns []*packetTestConn
	s, err := newPacketSink("udp", "collector.example.com:4739", time.Second, func(string, string) (net.Conn, error) {
		dialAttempts++
		if dialAttempts > 1 {
			return nil, resolveErr
		}
		conn := &packetTestConn{}
		conns = append(conns, conn)
		return conn, nil
	}, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatalf("newPacketSink returned error: %v", err)
	}

	if err := s.Send([]byte("first")); err != nil {
		t.Fatalf("first Send returned error: %v", err)
	}
	now = now.Add(time.Second)
	if err := s.Send([]byte("second")); err != nil {
		t.Fatalf("second Send returned error: %v", err)
	}

	if dialAttempts != 2 {
		t.Fatalf("expected scheduled refresh attempt, got %d dials", dialAttempts)
	}
	if len(conns) != 1 || conns[0].closed {
		t.Fatalf("expected original connection to remain open, conns=%d closed=%v", len(conns), conns[0].closed)
	}
	if len(conns[0].writes) != 2 || string(conns[0].writes[1]) != "second" {
		t.Fatalf("expected payload to use original connection after refresh failure, got %#v", conns[0].writes)
	}
}

func TestPacketSinkRefreshesAndRetriesAfterWriteError(t *testing.T) {
	writeErr := errors.New("write failed")
	var conns []*packetTestConn
	s, err := newPacketSink("udp", "collector.example.com:4739", time.Minute, func(string, string) (net.Conn, error) {
		conn := &packetTestConn{}
		if len(conns) == 0 {
			conn.writeErr = writeErr
		}
		conns = append(conns, conn)
		return conn, nil
	}, time.Now)
	if err != nil {
		t.Fatalf("newPacketSink returned error: %v", err)
	}

	if err := s.Send([]byte("retry")); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if len(conns) != 2 {
		t.Fatalf("expected write error to trigger a refresh, got %d dials", len(conns))
	}
	if !conns[0].closed {
		t.Fatalf("expected failed connection to be closed")
	}
	if len(conns[1].writes) != 1 || string(conns[1].writes[0]) != "retry" {
		t.Fatalf("expected retry payload on refreshed connection, got %#v", conns[1].writes)
	}
}

type packetTestConn struct {
	writes   [][]byte
	writeErr error
	closed   bool
}

func (c *packetTestConn) Read([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func (c *packetTestConn) Write(payload []byte) (int, error) {
	if c.writeErr != nil {
		return 0, c.writeErr
	}
	copied := append([]byte(nil), payload...)
	c.writes = append(c.writes, copied)
	return len(payload), nil
}

func (c *packetTestConn) Close() error {
	c.closed = true
	return nil
}

func (c *packetTestConn) LocalAddr() net.Addr {
	return packetTestAddr("local")
}

func (c *packetTestConn) RemoteAddr() net.Addr {
	return packetTestAddr("remote")
}

func (c *packetTestConn) SetDeadline(time.Time) error {
	return nil
}

func (c *packetTestConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *packetTestConn) SetWriteDeadline(time.Time) error {
	return nil
}

type packetTestAddr string

func (a packetTestAddr) Network() string {
	return string(a)
}

func (a packetTestAddr) String() string {
	return string(a)
}
