//go:build linux && !reflow_pcap && !reflow_dpdk

package netlinkcapture

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/sys/unix"
)

// Conn owns a netlink socket. Its poll timeout bounds cancellation latency.
type Conn struct {
	mu       sync.Mutex
	fd       int
	closed   bool
	sequence uint32
	buf      []byte
}

func NewConn() *Conn { return &Conn{fd: -1, buf: make([]byte, 256*1024)} }

func (c *Conn) Open(protocol, bufferSize int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return io.EOF
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, protocol)
	if err != nil {
		return err
	}
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		_ = unix.Close(fd)
		return err
	}
	if bufferSize > 0 {
		if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, bufferSize); err != nil {
			_ = unix.Close(fd)
			return err
		}
	}
	c.fd = fd
	return nil
}

func (c *Conn) FD() int { c.mu.Lock(); defer c.mu.Unlock(); return c.fd }

func (c *Conn) Closed() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.closed }

func (c *Conn) Join(group uint32) error {
	return unix.SetsockoptInt(c.FD(), unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, int(group))
}

func (c *Conn) Send(typ, flags uint16, payload []byte) (uint32, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fd < 0 {
		return 0, io.EOF
	}
	c.sequence++
	return c.sequence, unix.Sendto(c.fd, EncodeMessage(typ, flags, c.sequence, payload), 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK})
}

func (c *Conn) Read(ctx context.Context) ([]Message, error) {
	buf := c.buf
	for ctx.Err() == nil {
		fd := c.FD()
		if fd < 0 {
			return nil, io.EOF
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		_, err := unix.Poll(poll, 250)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if poll[0].Revents == 0 {
			continue
		}
		if poll[0].Revents&(unix.POLLNVAL|unix.POLLHUP) != 0 {
			if c.FD() < 0 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("netlink poll flags %#x", poll[0].Revents)
		}
		n, _, flags, _, err := unix.Recvmsg(fd, buf, nil, 0)
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if flags&unix.MSG_TRUNC != 0 {
			return nil, fmt.Errorf("truncated netlink datagram")
		}
		return Messages(buf[:n])
	}
	return nil, ctx.Err()
}

// Request is used before capture starts, so control replies cannot consume a
// packet from an already-running session.
func (c *Conn) Request(ctx context.Context, typ uint16, payload []byte) ([]Message, error) {
	sequence, err := c.Send(typ, unix.NLM_F_REQUEST|unix.NLM_F_ACK, payload)
	if err != nil {
		return nil, err
	}
	var replies []Message
	for {
		messages, err := c.Read(ctx)
		if err != nil {
			return nil, err
		}
		for _, msg := range messages {
			if msg.Sequence != sequence {
				continue
			}
			if msg.Type == unix.NLMSG_ERROR {
				if len(msg.Data) < 4 {
					return nil, fmt.Errorf("short netlink acknowledgement")
				}
				code := int32(binary.NativeEndian.Uint32(msg.Data))
				if code != 0 {
					return nil, unix.Errno(-code)
				}
				return replies, nil
			}
			if msg.Type == unix.NLMSG_DONE {
				return replies, nil
			}
			// The next receive reuses c.buf; control replies outlive that read.
			msg.Data = append([]byte(nil), msg.Data...)
			replies = append(replies, msg)
		}
	}
}

func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.fd < 0 {
		return nil
	}
	err := unix.Close(c.fd)
	c.fd = -1
	return err
}
