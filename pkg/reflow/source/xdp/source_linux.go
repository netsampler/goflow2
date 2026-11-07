//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

// Package xdp copies ingress packets to perf buffers while always returning
// XDP_PASS. It does not redirect packets or own an AF_XDP receive queue.
package xdp

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"golang.org/x/sys/unix"
)

type driver struct {
	cfg    config.SourceConfig
	index  int
	mu     sync.Mutex
	closed bool
	reader *perf.Reader
}

func New(cfg config.SourceConfig) (*capture.Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	if cfg.Capture.Direction != "ingress" {
		return nil, fmt.Errorf("xdp supports ingress capture only")
	}
	if cfg.Capture.Promiscuous != nil && *cfg.Capture.Promiscuous {
		return nil, fmt.Errorf("xdp cannot enable promiscuous mode")
	}
	if cfg.Capture.XDP.Mode == "" {
		cfg.Capture.XDP.Mode = "driver"
	}
	if cfg.Capture.XDP.Mode != "driver" && cfg.Capture.XDP.Mode != "generic" {
		return nil, fmt.Errorf("xdp mode must be driver or generic")
	}
	iface, err := net.InterfaceByName(cfg.Capture.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup XDP interface: %w", err)
	}
	return capture.New(cfg, &driver{cfg: cfg, index: iface.Index})
}

func (d *driver) Run(ctx context.Context, emit func(capture.Frame) error) (runErr error) {
	d.mu.Lock()
	closed := d.closed
	d.mu.Unlock()
	if ctx.Err() != nil || closed {
		return nil
	}
	events, err := ebpf.NewMap(&ebpf.MapSpec{Name: "reflow_packets", Type: ebpf.PerfEventArray, KeySize: 4, ValueSize: 4})
	if err != nil {
		return fmt.Errorf("create XDP perf map: %w", err)
	}
	defer func() { _ = events.Close() }()
	reader, err := perf.NewReader(events, os.Getpagesize()*32)
	if err != nil {
		return fmt.Errorf("open XDP perf reader: %w", err)
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		_ = reader.Close()
		return nil
	}
	d.reader = reader
	d.mu.Unlock()
	defer func() { _ = d.Close() }()
	// Perf event record lengths are uint16. Reserve room for perf framing and
	// our metadata even when the configured snapshot would exceed that bound.
	snaplen := min(d.cfg.Capture.SnapLen, 65480)
	var sampler *capture.PerfSampler
	if d.cfg.Capture.Sampling.Location == "kernel" {
		sampler, err = capture.NewPerfSampler(d.cfg.Capture, "xdp")
		if err != nil {
			return err
		}
		defer func() { _ = sampler.Close() }()
	}
	program, err := ebpf.NewProgram(&ebpf.ProgramSpec{Name: "reflow_pass", Type: ebpf.XDP, License: "GPL", Instructions: instructions(events.FD(), snaplen, sampler)})
	if err != nil {
		return fmt.Errorf("load passive XDP program: %w", err)
	}
	defer func() { _ = program.Close() }()
	mode := link.XDPDriverMode
	if d.cfg.Capture.XDP.Mode == "generic" {
		mode = link.XDPGenericMode
	}
	// A BPF link has exclusive ownership and attachment fails if the hook is
	// occupied. Closing this link cannot remove another owner's program.
	attachment, err := link.AttachXDP(link.XDPOptions{Program: program, Interface: d.index, Flags: mode})
	if err != nil {
		return fmt.Errorf("attach XDP without replacing an existing program: %w", err)
	}
	defer func() { _ = attachment.Close() }()
	var mono unix.Timespec
	if err = unix.ClockGettime(unix.CLOCK_MONOTONIC, &mono); err != nil {
		return err
	}
	boot := time.Now().UnixNano() - mono.Nano()
	stop := context.AfterFunc(ctx, func() { _ = d.Close() })
	defer stop()
	if sampler != nil {
		stopMonitor := sampler.Monitor(ctx, func() { _ = d.Close() })
		defer func() {
			if err := stopMonitor(); runErr == nil {
				runErr = err
			}
		}()
	}
	var lost uint64
	for ctx.Err() == nil {
		record, err := reader.Read()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, perf.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read XDP perf buffer: %w", err)
		}
		lost += record.LostSamples
		if record.LostSamples > 0 {
			continue
		}
		frame, err := decodeRecord(record.RawSample, boot)
		if err != nil {
			return err
		}
		frame.Interface = d.cfg.Capture.Interface
		frame.Direction = "ingress"
		frame.Drops = lost
		if sampler != nil {
			// Kernel helper failures already include perf-buffer overflow;
			// adding PERF_RECORD_LOST would count that loss twice.
			frame.Drops = sampler.Drops()
		} else {
			frame.Sampling = nil
		}
		frame.Fields = map[string]any{"capture_backend_snaplen": snaplen}
		if err = emit(frame); err != nil {
			return err
		}
	}
	return nil
}

func instructions(mapFD, snaplen int, sampler *capture.PerfSampler) asm.Instructions {
	insns := asm.Instructions{
		asm.Mov.Reg(asm.R6, asm.R1),
	}
	if sampler != nil {
		insns = append(insns, sampler.SelectInstructions(-4)...)
	} else {
		insns = append(insns, asm.StoreImm(asm.R10, -4, 0, asm.Word))
	}
	insns = append(insns, asm.Instructions{
		asm.LoadMem(asm.R7, asm.R6, 0, asm.Word), // data
		asm.LoadMem(asm.R8, asm.R6, 4, asm.Word), // data_end
		asm.Sub.Reg(asm.R8, asm.R7),
		asm.StoreMem(asm.R10, -24, asm.R8, asm.Word),
		asm.LoadMem(asm.R0, asm.R6, 12, asm.Word), // ingress_ifindex
		asm.StoreMem(asm.R10, -20, asm.R0, asm.Word),
		asm.FnKtimeGetNs.Call(),
		asm.StoreMem(asm.R10, -16, asm.R0, asm.DWord),
		asm.JLE.Imm(asm.R8, int32(snaplen), "length_ready"),
		asm.Mov.Imm(asm.R8, int32(snaplen)),
		asm.StoreMem(asm.R10, -8, asm.R8, asm.Word).WithSymbol("length_ready"),
		asm.LSh.Imm(asm.R8, 32),
		asm.LoadImm(asm.R3, 0xffffffff, asm.DWord),
		asm.Or.Reg(asm.R3, asm.R8),
		asm.Mov.Reg(asm.R1, asm.R6),
		asm.LoadMapPtr(asm.R2, mapFD),
		asm.Mov.Reg(asm.R4, asm.R10),
		asm.Add.Imm(asm.R4, -24),
		asm.Mov.Imm(asm.R5, 24),
		asm.FnPerfEventOutput.Call(),
	}...)
	if sampler != nil {
		insns = append(insns, sampler.OutputInstructions(2)...)
	} else {
		insns = append(insns, asm.Mov.Imm(asm.R0, 2), asm.Return())
	}
	return insns
}

func decodeRecord(data []byte, boot int64) (capture.Frame, error) {
	if len(data) < 24 {
		return capture.Frame{}, fmt.Errorf("short XDP perf record")
	}
	n := int(binary.NativeEndian.Uint32(data[16:]))
	if n > len(data)-24 {
		return capture.Frame{}, fmt.Errorf("short XDP packet data")
	}
	frame := capture.Frame{Data: data[24 : 24+n], LinkType: 1, InterfaceIndex: int(binary.NativeEndian.Uint32(data[4:])), CaptureLength: n, WireLength: int(binary.NativeEndian.Uint32(data)), Timestamp: time.Unix(0, boot+int64(binary.NativeEndian.Uint64(data[8:]))).UTC()}
	if rate := binary.NativeEndian.Uint32(data[20:]); rate > 0 {
		frame.Sampling = &event.SamplingMetadata{Rate: rate}
	}
	return frame, nil
}

func (d *driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.reader != nil {
		return d.reader.Close()
	}
	return nil
}
