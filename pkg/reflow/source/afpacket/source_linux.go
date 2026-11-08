//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

// Package afpacket implements packet capture without libpcap or cgo.
package afpacket

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"golang.org/x/sys/unix"
)

type driver struct {
	cfg    config.SourceConfig
	index  int
	closed atomic.Bool
}

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	iface, err := net.InterfaceByName(cfg.Capture.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup AF_PACKET interface: %w", err)
	}
	return capture.New(cfg, &driver{cfg: cfg, index: iface.Index})
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	if ctx.Err() != nil || d.closed.Load() {
		return nil
	}
	if d.cfg.Capture.Sampling.Adaptive.Enabled {
		return d.runAdaptive(ctx, emit)
	}
	session, err := openAFPacketRing(d.cfg.Capture, d.index)
	if err != nil {
		return err
	}
	defer session.close()
	fd, ring, blockSize, count := session.fd, session.ring, session.blockSize, session.count
	var drops uint64
	for blockIndex := 0; ctx.Err() == nil && !d.closed.Load(); {
		block := ring[blockIndex*blockSize : (blockIndex+1)*blockSize]
		status := (*uint32)(unsafe.Pointer(&block[8]))
		if atomic.LoadUint32(status)&unix.TP_STATUS_USER == 0 {
			_, err = unix.Poll([]unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}, 250)
			if err == unix.EINTR {
				continue
			}
			if err != nil {
				return fmt.Errorf("poll AF_PACKET: %w", err)
			}
			continue
		}
		if stats, e := unix.GetsockoptTpacketStatsV3(fd, unix.SOL_PACKET, unix.PACKET_STATISTICS); e == nil {
			drops += uint64(stats.Drops)
		}
		err = d.readBlock(block, drops, emit)
		atomic.StoreUint32(status, unix.TP_STATUS_KERNEL)
		if err != nil {
			return err
		}
		blockIndex = (blockIndex + 1) % count
	}
	return nil
}

func (d *driver) readBlock(block []byte, drops uint64, emit func(capture.Frame) error) error {
	if len(block) < 48 {
		return fmt.Errorf("short AF_PACKET block")
	}
	count := binary.NativeEndian.Uint32(block[12:])
	offset := int(binary.NativeEndian.Uint32(block[16:]))
	for n := uint32(0); n < count; n++ {
		if offset < 0 || offset > len(block)-68 {
			return fmt.Errorf("invalid AF_PACKET record offset")
		}
		h := block[offset:]
		caplen, wire := int(binary.NativeEndian.Uint32(h[12:])), int(binary.NativeEndian.Uint32(h[16:]))
		mac := int(binary.NativeEndian.Uint16(h[24:]))
		if mac < 48 || mac > len(h) || caplen > len(h)-mac {
			return fmt.Errorf("invalid AF_PACKET packet span")
		}
		packetType := h[58]
		direction := "ingress"
		if packetType == unix.PACKET_OUTGOING {
			direction = "egress"
		}
		if wanted := d.cfg.Capture.Direction; wanted != "both" && wanted != direction {
			goto next
		}
		{
			data := h[mac : mac+caplen]
			fields := map[string]any{}
			status := binary.NativeEndian.Uint32(h[20:])
			if status&unix.TP_STATUS_VLAN_VALID != 0 {
				tci := uint16(binary.NativeEndian.Uint32(h[32:]))
				tpid := uint16(0x8100)
				if status&unix.TP_STATUS_VLAN_TPID_VALID != 0 {
					tpid = binary.NativeEndian.Uint16(h[36:])
				}
				fields["capture_vlan_tci"] = tci
				fields["capture_vlan_tpid"] = tpid
				if len(data) >= 12 {
					restored := make([]byte, len(data)+4)
					copy(restored, data[:12])
					binary.BigEndian.PutUint16(restored[12:], tpid)
					binary.BigEndian.PutUint16(restored[14:], tci)
					copy(restored[16:], data[12:])
					data = restored
					caplen += 4
					wire += 4
				}
			}
			frame := capture.Frame{Data: data, Timestamp: time.Unix(int64(binary.NativeEndian.Uint32(h[4:])), int64(binary.NativeEndian.Uint32(h[8:]))).UTC(), LinkType: 1, Interface: d.cfg.Capture.Interface, InterfaceIndex: int(int32(binary.NativeEndian.Uint32(h[52:]))), Direction: direction, PacketType: packetTypeName(packetType), CaptureLength: caplen, WireLength: wire, Fields: fields, Drops: drops}
			if d.cfg.Capture.Sampling.Location == "kernel" {
				frame.Sampling = &event.SamplingMetadata{Rate: uint32(d.cfg.Capture.SampleEvery), Drops: uint32(drops)}
			}
			if err := emit(frame); err != nil {
				return err
			}
		}
	next:
		if n+1 < count {
			step := int(binary.NativeEndian.Uint32(h))
			if step < 68 || step > len(h) {
				return fmt.Errorf("invalid AF_PACKET next record")
			}
			offset += step
		}
	}
	return nil
}

func packetTypeName(v byte) string {
	switch v {
	case unix.PACKET_HOST:
		return "host"
	case unix.PACKET_BROADCAST:
		return "broadcast"
	case unix.PACKET_MULTICAST:
		return "multicast"
	case unix.PACKET_OTHERHOST:
		return "otherhost"
	case unix.PACKET_OUTGOING:
		return "outgoing"
	}
	return "unknown"
}
func hostToNetwork(v uint16) uint16 {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return binary.NativeEndian.Uint16(b[:])
}

// Run owns the mmap and descriptor. A bounded poll lets Close interrupt it
// without unmapping memory while another goroutine is reading a record.
func (d *driver) Close() error { d.closed.Store(true); return nil }

// socketFilter samples before packets consume ring capacity. SKF_AD_RANDOM is
// Linux's per-packet random ancillary load, so every packet can be selected
// independently of its flow. Rejected packets are only rejected by this socket.
func socketFilter(cfg config.CaptureConfig) []unix.SockFilter {
	accept := unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: uint32(cfg.SnapLen)}
	if cfg.Sampling.Location != "kernel" || cfg.SampleEvery <= 1 {
		return []unix.SockFilter{accept}
	}
	// linux/filter.h: SKF_AD_OFF=-0x1000, SKF_AD_RANDOM=56. The random
	// threshold avoids the larger modulo bias for non-power-of-two divisors.
	const randomOffset = uint32(0xfffff000 + 56)
	threshold := uint32((uint64(1) << 32) / uint64(cfg.SampleEvery))
	return []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: randomOffset},
		{Code: unix.BPF_JMP | unix.BPF_JGE | unix.BPF_K, K: threshold, Jt: 1},
		accept,
		{Code: unix.BPF_RET | unix.BPF_K},
	}
}
