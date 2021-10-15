package utils

import (
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
	dp := dummyFlowProcessor{}
	go func() {
		require.NoError(t, dp.FlowRoutine("127.0.0.1", port))
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
		case msg := <-dp.receivedMessages:
			return string(msg.(BaseMessage).Payload)
		case <-testTimeout:
			require.Fail(t, "test timed out while waiting for message")
			return ""
		}
	}

	// in UDP, messages might arrive out of order or duplicate, so whe just verify they arrive
	// to avoid flaky tests
	require.Contains(t, []string{"message 1", "message 2", "message 3"}, readMessage())
	require.Contains(t, []string{"message 1", "message 2", "message 3"}, readMessage())
	require.Contains(t, []string{"message 1", "message 2", "message 3"}, readMessage())

	dp.Shutdown()

	_ = sendMessage("no more messages should be processed")

	select {
	case msg := <-dp.receivedMessages:
		assert.Fail(t, fmt.Sprint(msg))
	default:
		// everything is correct
	}
}

type dummyFlowProcessor struct {
	stopper
	receivedMessages chan interface{}
}

func (d *dummyFlowProcessor) FlowRoutine(host string, port int) error {
	_ = d.start()
	d.receivedMessages = make(chan interface{})
	return UDPStoppableRoutine(d.stopCh, "test_udp", func(msg interface{}) error {
		d.receivedMessages <- msg
		return nil
	}, 3, host, port, false, logrus.StandardLogger())
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
