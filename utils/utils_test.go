package utils

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancelUDPRoutine(t *testing.T) {
	testTimeout := time.After(10 * time.Second)
	port, err := getFreeUDPPort()
	require.NoError(t, err)
	ctx, cancelContext := context.WithCancel(context.Background())
	receivedMessages := make(chan interface{})
	go func() {
		require.NoError(t, UDPRoutineWithCtx(ctx, "test_udp", func(msg interface{}) error {
			receivedMessages <- msg
			return nil
		}, 1, "127.0.0.1", port, false, logrus.StandardLogger()))
	}()

	// wait slightly so we give time to the server to accept requests
	time.Sleep(100 * time.Millisecond)

	sendMessage := func(msg string) error {
		conn, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return err
		}
		defer conn.Close()
		_, err = conn.Write([]byte(msg))
		return err
	}
	require.NoError(t, sendMessage("message 1"))
	require.NoError(t, sendMessage("message 2"))
	require.NoError(t, sendMessage("message 3"))

	readMessage := func() string {
		select {
		case msg := <-receivedMessages:
			return string(msg.(BaseMessage).Payload)
		case <-testTimeout:
			require.Fail(t, "test timed out while waiting for message")
			return ""
		}
	}

	require.Equal(t, "message 1", readMessage())
	require.Equal(t, "message 2", readMessage())
	require.Equal(t, "message 3", readMessage())

	cancelContext()

	_ = sendMessage("no more messages should be processed")

	select {
	case msg := <-receivedMessages:
		assert.Fail(t, fmt.Sprint(msg))
	default:
		// everything is correct
	}
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
