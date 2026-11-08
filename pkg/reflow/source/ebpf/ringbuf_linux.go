//go:build linux && !reflow_pcap && !reflow_dpdk

package ebpf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	bpf "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"golang.org/x/sys/unix"
)

// Keep the per-CPU scratch value (48-byte prefix plus packet) within the
// 32760-byte allocation supported by kernels with 32KiB per-CPU units.
// Snapshot truncation never changes the SKB wire length used for accounting.
const maxRingSnaplen = 32712

type ringCapture struct {
	events, scratch *bpf.Map
	sampler         *capture.PerfSampler
	program         *bpf.Program
	reader          *ringbuf.Reader
	snaplen         int
}

func (r *ringCapture) Close() {
	if r.reader != nil {
		_ = r.reader.Close()
	}
	if r.program != nil {
		_ = r.program.Close()
	}
	if r.sampler != nil {
		_ = r.sampler.Close()
	}
	if r.scratch != nil {
		_ = r.scratch.Close()
	}
	if r.events != nil {
		_ = r.events.Close()
	}
}

func (s *Source) prepareRingCapture() (_ *ringCapture, runErr error) {
	r := &ringCapture{snaplen: min(s.cfg.SnapLen, maxRingSnaplen)}
	defer func() {
		if runErr != nil {
			r.Close()
		}
	}()
	if s.cfg.EBPF.RingSize < os.Getpagesize() {
		return nil, fmt.Errorf("eBPF ring_size must be at least the kernel page size (%d)", os.Getpagesize())
	}
	var err error
	r.events, err = bpf.NewMap(&bpf.MapSpec{Name: "reflow_packets", Type: bpf.RingBuf, MaxEntries: uint32(s.cfg.EBPF.RingSize)})
	if err != nil {
		return nil, fmt.Errorf("create eBPF ring buffer: %w", err)
	}
	r.reader, err = ringbuf.NewReader(r.events)
	if err != nil {
		return nil, fmt.Errorf("open eBPF ring reader: %w", err)
	}
	r.scratch, err = bpf.NewMap(&bpf.MapSpec{Name: "reflow_scratch", Type: bpf.PerCPUArray, KeySize: 4, ValueSize: uint32(48 + r.snaplen), MaxEntries: 1})
	if err != nil {
		return nil, fmt.Errorf("create eBPF ring scratch buffer: %w", err)
	}
	cfg := s.cfg.Capture
	if cfg.Sampling.Location != "kernel" {
		// The shared capture adapter owns userspace systematic selection. The
		// kernel still counts copy/output failures, but delivers every packet.
		cfg.SampleEvery = 1
		cfg.Sampling.Adaptive.Enabled = false
	}
	r.sampler, err = capture.NewPerfSampler(cfg, "ebpf")
	if err != nil {
		return nil, err
	}
	load := func(full bool) (*bpf.Program, error) {
		return bpf.NewProgram(&bpf.ProgramSpec{Name: "reflow_capture", Type: bpf.SocketFilter, License: "GPL", Instructions: ringSocketInstructions(r.events.FD(), r.scratch.FD(), r.snaplen, full, r.sampler)})
	}
	r.program, err = load(true)
	if err != nil {
		r.program, err = load(false)
	}
	if err != nil {
		return nil, fmt.Errorf("load eBPF ring-buffer socket filter: %w", err)
	}
	return r, nil
}

func (s *Source) runRingCapture(ctx context.Context, fd int, r *ringCapture, emit func(*event.Event) error) (runErr error) {
	defer r.Close()
	// Close the packet socket before releasing maps still referenced by its hook.
	defer func() { _ = s.closeBackend() }()
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, r.program.FD()); err != nil {
		return fmt.Errorf("attach eBPF ring-buffer filter: %w", err)
	}
	s.mu.Lock()
	s.kernelSampler = r.sampler
	s.backendSnaplen = r.snaplen
	s.mu.Unlock()
	slog.Info("capture transport selected", "network", "ebpf", "interface", s.cfg.Interface, "transport", "ringbuf", "snaplen", r.snaplen, "ring_size", s.cfg.EBPF.RingSize)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = r.reader.Close() })
	defer stop()
	stopMonitor := r.sampler.Monitor(ctx, cancel)
	defer func() {
		if err := stopMonitor(); runErr == nil {
			runErr = err
		}
	}()
	var record ringbuf.Record
	for ctx.Err() == nil {
		if err := r.reader.ReadInto(&record); err != nil {
			if ctx.Err() != nil || errors.Is(err, ringbuf.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read eBPF ring buffer: %w", err)
		}
		if err := s.emitRingSample(record.RawSample, emit); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) emitRingSample(sample []byte, emit func(*event.Event) error) error {
	if len(sample) < 48 {
		return fmt.Errorf("short eBPF ring record")
	}
	if s.cfg.Capture.Sampling.Location != "kernel" {
		sample = sample[4:]
	}
	return s.emitPerfSample(sample, emit)
}

// Ring records use the same rate + SKB prefix as sampled perf records. Packet
// bytes are copied after kernel selection, and only initialized bytes are sent.
func ringSocketInstructions(eventsFD, scratchFD, snaplen int, full bool, sampler *capture.PerfSampler) asm.Instructions {
	ins := asm.Instructions{asm.Mov.Reg(asm.R6, asm.R1)}
	ins = append(ins, sampler.SelectInstructions(-48)...)
	ins = append(ins, asm.Instructions{
		asm.LoadMapPtr(asm.R1, scratchFD), asm.Mov.Reg(asm.R2, asm.R10), asm.Add.Imm(asm.R2, -56), asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "ring_empty"), asm.Mov.Reg(asm.R7, asm.R0),
		asm.LoadMem(asm.R1, asm.R10, -48, asm.Word), asm.StoreMem(asm.R7, 0, asm.R1, asm.Word),
	}...)
	for i, off := range []int16{skbLenOffset, skbPacketTypeOffset, skbMarkOffset, skbQueueMappingOffset, skbProtocolOffset, skbPriorityOffset, skbIngressIfindexOffset, skbIfindexOffset, skbTCIndexOffset, skbHashOffset, skbTCClassIDOffset} {
		dst := int16(4 + i*4)
		if !full && (off == skbTCIndexOffset || off == skbTCClassIDOffset) {
			ins = append(ins, asm.StoreImm(asm.R7, dst, 0, asm.Word))
		} else {
			ins = append(ins, asm.LoadMem(asm.R1, asm.R6, off, asm.Word), asm.StoreMem(asm.R7, dst, asm.R1, asm.Word))
		}
	}
	ins = append(ins, asm.Instructions{
		asm.LoadMem(asm.R8, asm.R6, skbLenOffset, asm.Word), asm.JLE.Imm(asm.R8, int32(snaplen), "ring_length"), asm.Mov.Imm(asm.R8, int32(snaplen)),
		asm.Mov.Reg(asm.R1, asm.R6).WithSymbol("ring_length"), asm.Mov.Imm(asm.R2, 0), asm.Mov.Reg(asm.R3, asm.R7), asm.Add.Imm(asm.R3, 48), asm.Mov.Reg(asm.R4, asm.R8),
		// Check after the clamp: older verifiers lose the positive lower bound
		// across an earlier !=0 check followed by the upper-bound branch.
		asm.JLT.Imm(asm.R4, 1, "ring_empty"), asm.FnSkbLoadBytes.Call(), asm.JNE.Imm(asm.R0, 0, "ring_output_done"),
		asm.LoadMapPtr(asm.R1, eventsFD), asm.Mov.Reg(asm.R2, asm.R7), asm.Mov.Reg(asm.R3, asm.R8), asm.Add.Imm(asm.R3, 48), asm.Mov.Imm(asm.R4, 0), asm.FnRingbufOutput.Call(),
		asm.Ja.Label("ring_output_done"), asm.Mov.Imm(asm.R0, -int32(unix.EFAULT)).WithSymbol("ring_empty"),
	}...)
	output := sampler.OutputInstructions(0)
	output[0] = output[0].WithSymbol("ring_output_done")
	return append(ins, output...)
}
