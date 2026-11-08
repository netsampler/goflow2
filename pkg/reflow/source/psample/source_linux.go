//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package psample

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/netlinkcapture"
	"golang.org/x/sys/unix"
)

type driver struct {
	cfg      config.SourceConfig
	conn     *netlinkcapture.Conn
	decoder  *decoder
	overruns uint64
}

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	if cfg.Capture.Direction != "both" {
		return nil, fmt.Errorf("psample direction is selected by sample groups; use capture.direction=both")
	}
	if cfg.Capture.Promiscuous != nil && *cfg.Capture.Promiscuous {
		return nil, fmt.Errorf("psample cannot enable promiscuous mode")
	}
	return capture.New(cfg, &driver{cfg: cfg, conn: netlinkcapture.NewConn(), decoder: newDecoder(cfg.Capture.PSample.Groups, cfg.Capture.PSample.PortMap)})
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	if ctx.Err() != nil {
		return nil
	}
	if err := d.conn.Open(unix.NETLINK_GENERIC, d.cfg.Capture.PSample.BufferSize); err != nil {
		if d.conn.Closed() {
			return nil
		}
		return fmt.Errorf("open psample: %w", err)
	}
	defer func() { _ = d.Close() }()
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	payload := append([]byte{3, 1, 0, 0}, netlinkcapture.Attribute(2, []byte("psample\x00"))...)
	replies, err := d.conn.Request(startup, unix.GENL_ID_CTRL, payload)
	if err != nil {
		if ctx.Err() != nil || d.conn.Closed() {
			return nil
		}
		return fmt.Errorf("resolve psample family (load psample and configure its producer): %w", err)
	}
	family, group, err := resolveFamily(replies)
	if err != nil {
		return err
	}
	if err = d.conn.Join(group); err != nil {
		return fmt.Errorf("join psample packets multicast: %w", err)
	}
	for ctx.Err() == nil {
		messages, err := d.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil || d.conn.FD() < 0 {
				return nil
			}
			if err == unix.ENOBUFS {
				d.overruns++
				continue
			}
			return fmt.Errorf("read psample: %w", err)
		}
		for _, message := range messages {
			if message.Type != family {
				continue
			}
			frame, ok, err := d.decoder.packet(message.Data)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			frame.Fields["capture_netlink_overruns"] = d.overruns
			if err = emit(frame); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveFamily(replies []netlinkcapture.Message) (uint16, uint32, error) {
	for _, reply := range replies {
		if len(reply.Data) < 4 {
			continue
		}
		attrs, err := netlinkcapture.Attributes(reply.Data[4:])
		if err != nil {
			return 0, 0, err
		}
		if len(attrs[1]) < 2 {
			continue
		}
		family := binary.NativeEndian.Uint16(attrs[1])
		groups, err := netlinkcapture.Attributes(attrs[7])
		if err != nil {
			return 0, 0, err
		}
		for _, entry := range groups {
			a, err := netlinkcapture.Attributes(entry)
			if err != nil {
				return 0, 0, err
			}
			if string(a[1]) == "packets\x00" && len(a[2]) >= 4 {
				return family, binary.NativeEndian.Uint32(a[2]), nil
			}
		}
	}
	return 0, 0, fmt.Errorf("psample family has no packets multicast group")
}

func (d *driver) Close() error { return d.conn.Close() }
