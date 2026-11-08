//go:build linux && !reflow_pcap && !reflow_dpdk

package capture

import (
	"math"
	"os"
	"testing"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
)

func TestExtendPerfSamplingStats32(t *testing.T) {
	before := []PerfSamplingStats{{Selected: math.MaxUint32 - 2, Failed: math.MaxUint32}, {Selected: 10, Failed: 4}}
	total := extendPerfSamplingStats32(before, nil, PerfSamplingStats{})
	after := []PerfSamplingStats{{Selected: 3, Failed: 2}, {Selected: 15, Failed: 5}}
	got := extendPerfSamplingStats32(after, before, total)
	if got.Selected != total.Selected+11 || got.Failed != total.Failed+4 {
		t.Fatalf("wrapped per-CPU totals: before=%+v after=%+v", total, got)
	}
}

func TestPerfSamplingARM32InstructionsIntegration(t *testing.T) {
	if os.Getenv("REFLOW_EBPF_INTEGRATION") != "1" {
		t.Skip("set REFLOW_EBPF_INTEGRATION=1 with BPF syscall access")
	}
	// Exercise the actual emitted 32-bit arithmetic, including arbitrary divisors
	// and the power-of-two correction needed to preserve the sampling probability.
	for _, every := range []uint32{2, 3, 16, 64, 128, 256, 1000, math.MaxUint32} {
		insns := asm.Instructions{asm.Mov.Imm32(asm.R7, int32(every))}
		insns = append(insns, perfSamplingThresholdInstructions(true)...)
		insns = append(insns, asm.Mov.Reg(asm.R0, asm.R8), asm.Return())
		p, err := ebpf.NewProgram(&ebpf.ProgramSpec{Type: ebpf.SocketFilter, License: "GPL", Instructions: insns})
		if err != nil {
			t.Fatalf("divisor %d: %+v", every, err)
		}
		got, _, err := p.Test(make([]byte, 64))
		_ = p.Close()
		if err != nil {
			t.Fatal(err)
		}
		if want := uint32(uint64(1) << 32 / uint64(every)); got != want {
			t.Fatalf("divisor %d: threshold=%d want=%d", every, got, want)
		}
	}

	// Run ARM32's non-atomic per-CPU increments through a real BPF map,
	// then extend a wrap in both counters without losing the accumulated total.
	m, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.PerCPUArray, KeySize: 4, ValueSize: 16, MaxEntries: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	n, err := ebpf.PossibleCPU()
	if err != nil {
		t.Fatal(err)
	}
	before := make([]PerfSamplingStats, n)
	for i := range before {
		before[i] = PerfSamplingStats{Selected: math.MaxUint32 - 2, Failed: math.MaxUint32 - 4}
	}
	if err := m.Put(uint32(0), before); err != nil {
		t.Fatal(err)
	}
	insns := asm.Instructions{
		asm.StoreImm(asm.R10, -4, 0, asm.Word),
		asm.LoadMapPtr(asm.R1, m.FD()),
		asm.Mov.Reg(asm.R2, asm.R10), asm.Add.Imm(asm.R2, -4),
		asm.FnMapLookupElem.Call(), asm.JEq.Imm(asm.R0, 0, "done"),
		asm.Mov.Reg(asm.R9, asm.R0),
	}
	insns = append(insns, perfSamplingIncrementInstructions(0, true)...)
	insns = append(insns, perfSamplingIncrementInstructions(8, true)...)
	insns = append(insns, asm.Mov.Imm(asm.R0, 0).WithSymbol("done"), asm.Return())
	p, err := ebpf.NewProgram(&ebpf.ProgramSpec{Type: ebpf.SocketFilter, License: "GPL", Instructions: insns})
	if err != nil {
		t.Fatalf("ARM32 counters: %+v", err)
	}
	defer func() { _ = p.Close() }()
	if _, err := p.Run(&ebpf.RunOptions{Data: make([]byte, 64), Repeat: 11}); err != nil {
		t.Fatal(err)
	}
	var after []PerfSamplingStats
	if err := m.Lookup(uint32(0), &after); err != nil {
		t.Fatal(err)
	}
	got := extendPerfSamplingStats32(after, before, PerfSamplingStats{})
	if got.Selected != 11 || got.Failed != 11 {
		t.Fatalf("counter delta: %+v", got)
	}
}
