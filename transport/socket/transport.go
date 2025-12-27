// Package socket implements a socket transport (TCP/UDP/Unix).
package socket

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v2/transport"
)

// SocketDriver sends formatted messages to a socket endpoint.
type SocketDriver struct {
	socketNetwork  string
	socketAddress  string
	lineSeparator  string
	connectTimeout time.Duration

	conn net.Conn
	lock *sync.RWMutex
}

// Prepare registers flags for socket transport configuration.
func (d *SocketDriver) Prepare() error {
	flag.StringVar(&d.socketNetwork, "transport.socket.network", "unix", "Socket network (tcp, udp, unix, unixgram, unixpacket)")
	flag.StringVar(&d.socketAddress, "transport.socket.address", "/tmp/goflow2.sock", "Socket address (host:port or unix path)")
	flag.StringVar(&d.lineSeparator, "transport.socket.sep", "\n", "Line separator")
	flag.DurationVar(&d.connectTimeout, "transport.socket.timeout", time.Second*5, "Socket connect timeout (0 for no timeout)")
	return nil
}

func (d *SocketDriver) isDatagram() bool {
	switch strings.ToLower(d.socketNetwork) {
	case "udp", "udp4", "udp6", "unixgram", "unixpacket":
		return true
	default:
		return false
	}
}

func (d *SocketDriver) dial() (net.Conn, error) {
	if d.socketAddress == "" {
		return nil, fmt.Errorf("socket address is empty")
	}
	network := strings.ToLower(d.socketNetwork)
	dialer := net.Dialer{Timeout: d.connectTimeout}
	return dialer.Dial(network, d.socketAddress)
}

func (d *SocketDriver) ensureConn() (net.Conn, error) {
	d.lock.RLock()
	conn := d.conn
	d.lock.RUnlock()
	if conn != nil {
		return conn, nil
	}

	d.lock.Lock()
	defer d.lock.Unlock()
	if d.conn != nil {
		return d.conn, nil
	}

	conn, err := d.dial()
	if err != nil {
		return nil, err
	}
	d.conn = conn
	return conn, nil
}

func (d *SocketDriver) resetConn() {
	d.lock.Lock()
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}
	d.lock.Unlock()
}

// Init establishes the socket connection.
func (d *SocketDriver) Init() error {
	_, err := d.ensureConn()
	return err
}

func (d *SocketDriver) write(conn net.Conn, data []byte) error {
	if len(data) == 0 && d.lineSeparator == "" {
		return nil
	}

	if d.lineSeparator == "" {
		_, err := conn.Write(data)
		return err
	}

	if len(data) == 0 {
		_, err := conn.Write([]byte(d.lineSeparator))
		return err
	}

	if d.isDatagram() {
		buf := make([]byte, 0, len(data)+len(d.lineSeparator))
		buf = append(buf, data...)
		buf = append(buf, d.lineSeparator...)
		_, err := conn.Write(buf)
		return err
	}

	if _, err := conn.Write(data); err != nil {
		return err
	}
	_, err := conn.Write([]byte(d.lineSeparator))
	return err
}

// Send writes a formatted message to the socket, reconnecting on failures.
func (d *SocketDriver) Send(key, data []byte) error {
	conn, err := d.ensureConn()
	if err != nil {
		return err
	}
	if err := d.write(conn, data); err == nil {
		return nil
	}

	d.resetConn()
	conn, err = d.ensureConn()
	if err != nil {
		return err
	}
	return d.write(conn, data)
}

// Close closes the socket connection.
func (d *SocketDriver) Close() error {
	d.resetConn()
	return nil
}

func init() {
	d := &SocketDriver{
		lock: &sync.RWMutex{},
	}
	transport.RegisterTransportDriver("socket", d)
}
