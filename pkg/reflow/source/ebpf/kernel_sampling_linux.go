//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package ebpf

import (
	"context"
	"fmt"

	cilium "github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"golang.org/x/sys/unix"
)

func (s *Source) runKernelSampling(ctx context.Context, fd, eventMapFD int, readers []*perfEventReader, emit func(*event.Event) error) error {
	sampler, err := capture.NewPerfSampler(s.cfg.Capture, "ebpf")
	if err != nil {
		return err
	}
	defer func() { _ = sampler.Close() }()
	s.mu.Lock()
	s.kernelSampler = sampler
	s.mu.Unlock()
	load := func(fullMetadata bool) (*cilium.Program, error) {
		return cilium.NewProgram(&cilium.ProgramSpec{Name: "reflow_capture", Type: cilium.SocketFilter, License: "GPL",
			Instructions: sampledSocketInstructions(eventMapFD, min(s.cfg.SnapLen, 65464), fullMetadata, sampler)})
	}
	program, err := load(true)
	if err != nil {
		program, err = load(false)
	}
	if err != nil {
		return fmt.Errorf("load sampled ebpf socket filter: %w", err)
	}
	defer func() { _ = program.Close() }()
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ATTACH_BPF, program.FD()); err != nil {
		return fmt.Errorf("attach sampled ebpf socket filter: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopMonitor := sampler.Monitor(ctx, cancel)
	defer func() { _ = stopMonitor() }()
	err = s.pollPerfEvents(ctx, readers, emit)
	if monitorErr := stopMonitor(); err == nil {
		err = monitorErr
	}
	return err
}

// Sampled records prefix the existing 44-byte SKB metadata with a uint32
// divisor. This leaves the legacy userspace-sampling record format unchanged.
func sampledSocketInstructions(eventsFD, snaplen int, fullMetadata bool, sampler *capture.PerfSampler) asm.Instructions {
	insns := asm.Instructions{asm.Mov.Reg(asm.R6, asm.R1)}
	insns = append(insns, sampler.SelectInstructions(-48)...)
	fields := []int16{skbLenOffset, skbPacketTypeOffset, skbMarkOffset,
		skbQueueMappingOffset, skbProtocolOffset, skbPriorityOffset,
		skbIngressIfindexOffset, skbIfindexOffset, skbTCIndexOffset, skbHashOffset, skbTCClassIDOffset}
	for i, offset := range fields {
		dst := int16(-44 + i*4)
		if !fullMetadata && (offset == skbTCIndexOffset || offset == skbTCClassIDOffset) {
			insns = append(insns, asm.StoreImm(asm.R10, dst, 0, asm.Word))
		} else {
			insns = append(insns, asm.LoadMem(asm.R1, asm.R6, offset, asm.Word), asm.StoreMem(asm.R10, dst, asm.R1, asm.Word))
		}
	}
	insns = append(insns, asm.Instructions{
		asm.LoadMem(asm.R3, asm.R6, skbLenOffset, asm.Word),
		asm.JLE.Imm(asm.R3, int32(snaplen), "length_ready"),
		asm.Mov.Imm(asm.R3, int32(snaplen)),
		asm.LSh.Imm(asm.R3, 32).WithSymbol("length_ready"),
		asm.LoadImm(asm.R7, 0xffffffff, asm.DWord),
		asm.Or.Reg(asm.R3, asm.R7),
		asm.LoadMapPtr(asm.R2, eventsFD),
		asm.Mov.Reg(asm.R1, asm.R6),
		asm.Mov.Reg(asm.R4, asm.R10),
		asm.Add.Imm(asm.R4, -48),
		asm.Mov.Imm(asm.R5, 48),
		asm.FnPerfEventOutput.Call(),
	}...)
	return append(insns, sampler.OutputInstructions(0)...)
}
