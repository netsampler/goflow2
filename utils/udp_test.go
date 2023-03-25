package utils

import (
	"fmt"
	"net"
	"testing"
	"time"

	//"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUDPReceiver(t *testing.T) {
	addr := "[::1]"
	port, err := getFreeUDPPort()
	require.NoError(t, err)
	t.Logf("starting UDP receiver on %s:%d\n", addr, port)

	r := NewUDPReceiver(nil)
	r.Start(1, addr, port)

	sendMessage := func(msg string) error {
		conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", addr, port))
		if err != nil {
			return err
		}
		defer conn.Close()
		_, err = conn.Write([]byte(msg))
		return err
	}
	require.NoError(t, sendMessage("message 1"))
	t.Log("sending message\n")
	<-time.After(time.Second)
	r.Stop()
}
