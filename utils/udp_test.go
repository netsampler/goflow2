package utils

import (
	"fmt"
	"net"
	"testing"

	//"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUDPReceiver(t *testing.T) {
	addr := "[::1]"
	port, err := getFreeUDPPort()
	require.NoError(t, err)
	t.Logf("starting UDP receiver on %s:%d\n", addr, port)

	r, err := NewUDPReceiver(nil)
	require.NoError(t, err)

	require.NoError(t, r.Start(addr, port, nil))
	sendMessage := func(msg string) error {
		conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", addr, port))
		if err != nil {
			return err
		}
		defer conn.Close()
		_, err = conn.Write([]byte(msg))
		return err
	}
	require.NoError(t, sendMessage("message"))
	t.Log("sending message\n")
	require.NoError(t, r.Stop())
}

func TestUDPClose(t *testing.T) {
	addr := "[::1]"
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

func getFreeUDPPort() (int, error) {
	a, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenUDP("udp", a)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.LocalAddr().(*net.UDPAddr).Port, nil
}
