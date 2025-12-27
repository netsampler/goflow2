package utils

import (
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	//"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUDPReceiver(t *testing.T) {
	addr := "::1"
	port, err := getFreeUDPPort()
	require.NoError(t, err)
	t.Logf("starting UDP receiver on %s:%d\n", addr, port)

	r, err := NewUDPReceiver(nil)
	require.NoError(t, err)

	require.NoError(t, r.Start(addr, port, nil))
	sendMessage := func(msg string) error {
		conn, err := net.Dial("udp", net.JoinHostPort(addr, strconv.Itoa(port)))
		if err != nil {
			return err
		}
		_, err = conn.Write([]byte(msg))
		if err != nil {
			if closeErr := conn.Close(); closeErr != nil {
				return closeErr
			}
			return err
		}
		if err := conn.Close(); err != nil {
			return err
		}
		return nil
	}
	require.NoError(t, sendMessage("message"))
	t.Log("sending message\n")
	require.NoError(t, r.Stop())
}

func TestUDPClose(t *testing.T) {
	addr := "::1"
	port, err := getFreeUDPPort()
	require.NoError(t, err)
	t.Logf("starting UDP receiver on %s:%d\n", addr, port)

	r, err := NewUDPReceiver(nil)
	require.NoError(t, err)
	require.NoError(t, r.Start(addr, port, nil))
	require.NoError(t, r.Stop())
	require.NoError(t, r.Start(addr, port, nil))
	require.Error(t, r.Start(addr, port, nil))
	require.NoError(t, r.Stop())
	require.Error(t, r.Stop())
}

func TestUDPReceiverDrainOnStop(t *testing.T) {
	cfg := &UDPReceiverConfig{
		Workers:   1,
		Sockets:   1,
		QueueSize: 1000,
	}
	r, err := NewUDPReceiver(cfg)
	require.NoError(t, err)

	var decoded atomic.Int64
	decodeFunc := func(msg interface{}) error {
		decoded.Add(1)
		time.Sleep(2 * time.Millisecond) // slow decode to ensure backlog exists
		return nil
	}

	total := 50
	r.ready = make(chan bool) // mark as started without opening sockets
	require.NoError(t, r.decoders(cfg.Workers, decodeFunc))
	for i := 0; i < total; i++ {
		r.dispatch <- &udpPacket{
			src:      &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234},
			dst:      &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5678},
			size:     1,
			payload:  []byte{1},
			received: time.Now().UTC(),
		}
	}

	require.NoError(t, r.Stop())
	require.EqualValues(t, total, decoded.Load())
}

func getFreeUDPPort() (int, error) {
	a, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenUDP("udp", a)
	if err != nil {
		return 0, err
	}
	port := l.LocalAddr().(*net.UDPAddr).Port
	if err := l.Close(); err != nil {
		return 0, err
	}
	return port, nil
}
