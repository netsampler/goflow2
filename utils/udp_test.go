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

	r := NewUDPReceiver(nil)
	r.Start(addr, port)

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
	r.Stop()
}

func TestUDPClose(t *testing.T) {
	addr := "[::1]"
	port, err := getFreeUDPPort()
	require.NoError(t, err)
	t.Logf("starting UDP receiver on %s:%d\n", addr, port)

	r := NewUDPReceiver(nil)
	r.Start(addr, port)
	r.Stop()
	r.Start(addr, port)
	r.Start(addr, port)
	r.Stop()
}
