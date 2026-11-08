//go:build linux && !reflow_pcap && !reflow_dpdk

package nflog

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
	cfg  config.SourceConfig
	conn *netlinkcapture.Conn
	loss uint64
}

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	if cfg.Capture.Direction != "both" {
		return nil, fmt.Errorf("NFLOG direction is selected by firewall hooks; use capture.direction=both")
	}
	if cfg.Capture.Promiscuous != nil && *cfg.Capture.Promiscuous {
		return nil, fmt.Errorf("NFLOG cannot enable promiscuous mode")
	}
	return capture.New(cfg, &driver{cfg: cfg, conn: netlinkcapture.NewConn()})
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	if ctx.Err() != nil {
		return nil
	}
	if err := d.conn.Open(unix.NETLINK_NETFILTER, d.cfg.Capture.NFLog.BufferSize); err != nil {
		if d.conn.Closed() {
			return nil
		}
		return fmt.Errorf("open NFLOG: %w", err)
	}
	defer func() { _ = d.Close() }()
	group := d.cfg.Capture.NFLog.Group
	header := []byte{unix.AF_UNSPEC, 0, byte(group >> 8), byte(group)}
	startup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Bind only this group. Never unbind another subscriber or take over the
	// protocol-family logging handler system-wide.
	cmd := append(append([]byte(nil), header...), netlinkcapture.Attribute(1, []byte{1, 0, 0, 0})...)
	if _, err := d.conn.Request(startup, 4<<8|1, cmd); err != nil {
		if ctx.Err() != nil || d.conn.Closed() {
			return nil
		}
		return fmt.Errorf("bind NFLOG group %d: %w", group, err)
	}
	mode := make([]byte, 6)
	binary.BigEndian.PutUint32(mode, uint32(d.cfg.Capture.SnapLen))
	mode[4] = 2
	options := append(append([]byte(nil), header...), netlinkcapture.Attribute(2, mode)...)
	if _, err := d.conn.Request(startup, 4<<8|1, options); err != nil {
		if ctx.Err() != nil || d.conn.Closed() {
			return nil
		}
		return fmt.Errorf("configure NFLOG group %d: %w", group, err)
	}
	for ctx.Err() == nil {
		messages, err := d.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil || d.conn.FD() < 0 {
				return nil
			}
			if err == unix.ENOBUFS {
				d.loss++
				continue
			}
			return fmt.Errorf("read NFLOG: %w", err)
		}
		for _, msg := range messages {
			if msg.Type != 4<<8 {
				continue
			}
			frame, err := decodePacket(msg.Data)
			if err != nil {
				return err
			}
			frame.Fields["capture_netlink_overruns"] = d.loss
			if err = emit(frame); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *driver) Close() error { return d.conn.Close() }
