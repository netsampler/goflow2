//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package capture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

// PerfSamplingStats counts selected copy attempts and failed perf-output calls.
// Intentional random exclusions are absent from both counters. Failed calls and
// PERF_RECORD_LOST describe overlapping loss, so callers must not add them.
type PerfSamplingStats struct {
	Selected uint64
	Failed   uint64
}

// PerfSampler owns one source's divisor and per-CPU delivery counters. The
// aligned uint32 divisor is read once per packet and copied into its record.
type PerfSampler struct {
	control *ebpf.Map
	stats   *ebpf.Map
	cfg     config.CaptureConfig
	network string
	drops   atomic.Uint64
	rate    atomic.Uint32
}

func NewPerfSampler(cfg config.CaptureConfig, network string) (*PerfSampler, error) {
	control, err := ebpf.NewMap(&ebpf.MapSpec{Name: "reflow_rate", Type: ebpf.Array, KeySize: 4, ValueSize: 4, MaxEntries: 1})
	if err != nil {
		return nil, fmt.Errorf("create capture sampling divisor: %w", err)
	}
	s := &PerfSampler{control: control, cfg: cfg, network: network}
	if err := s.SetRate(uint32(cfg.SampleEvery)); err != nil {
		_ = control.Close()
		return nil, err
	}
	s.stats, err = ebpf.NewMap(&ebpf.MapSpec{Name: "reflow_delivery", Type: ebpf.PerCPUArray, KeySize: 4, ValueSize: 16, MaxEntries: 1})
	if err != nil {
		_ = control.Close()
		return nil, fmt.Errorf("create capture sampling counters: %w", err)
	}
	return s, nil
}

func (s *PerfSampler) Close() error  { return errors.Join(s.control.Close(), s.stats.Close()) }
func (s *PerfSampler) Drops() uint64 { return s.drops.Load() }
func (s *PerfSampler) Rate() uint32  { return s.rate.Load() }

func (s *PerfSampler) SetRate(rate uint32) error {
	if rate == 0 {
		return fmt.Errorf("capture sampling divisor must be positive")
	}
	if err := s.control.Update(uint32(0), rate, ebpf.UpdateAny); err != nil {
		return err
	}
	s.rate.Store(rate)
	return nil
}

func (s *PerfSampler) ReadStats() (PerfSamplingStats, error) {
	var cpus []PerfSamplingStats
	if err := s.stats.Lookup(uint32(0), &cpus); err != nil {
		return PerfSamplingStats{}, fmt.Errorf("read capture sampling counters: %w", err)
	}
	var total PerfSamplingStats
	for _, cpu := range cpus {
		total.Selected += cpu.Selected
		total.Failed += cpu.Failed
	}
	s.drops.Store(total.Failed)
	return total, nil
}

// Monitor remains independent of packet delivery even when emit blocks. On a
// map failure interrupt wakes the reader; stop waits for monitor ownership to
// end before the caller closes maps and reports the failure.
func (s *PerfSampler) Monitor(ctx context.Context, interrupt func()) (stop func() error) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	var monitorErr error
	go func() {
		defer close(done)
		interval := time.Second
		if s.cfg.Sampling.Adaptive.Enabled {
			interval = time.Duration(s.cfg.Sampling.Adaptive.IntervalMS) * time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		controller := NewSamplingController(s.cfg.Sampling.Adaptive, uint32(s.cfg.SampleEvery))
		var previous PerfSamplingStats
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				current, err := s.ReadStats()
				if err != nil {
					monitorErr = err
					interrupt()
					return
				}
				oldRate := controller.Rate()
				newRate := controller.Observe(now, current.Failed-previous.Failed, current.Selected-previous.Selected)
				previous = current
				if newRate != oldRate {
					if err := s.SetRate(newRate); err != nil {
						monitorErr = fmt.Errorf("update capture sampling divisor: %w", err)
						interrupt()
						return
					}
					slog.Info("capture sampling rate changed", "network", s.network, "interface", s.cfg.Interface,
						"previous_sample_every", oldRate, "sample_every", newRate, "total_dropped", current.Failed)
				}
			}
		}
	}()
	return func() error { cancel(); <-done; return monitorErr }
}

// SelectInstructions expects the capture context in R6. It preserves R6,
// stores the selected divisor at rateOffset, and keeps the stats pointer in R9
// until OutputInstructions. Reserve stack [-56,-52) for its array lookup key.
func (s *PerfSampler) SelectInstructions(rateOffset int16) asm.Instructions {
	return asm.Instructions{
		asm.StoreImm(asm.R10, -56, 0, asm.Word),
		asm.LoadMapPtr(asm.R1, s.control.FD()),
		asm.Mov.Reg(asm.R2, asm.R10),
		asm.Add.Imm(asm.R2, -56),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "sampling_exit"),
		asm.LoadMem(asm.R7, asm.R0, 0, asm.Word),
		asm.JEq.Imm(asm.R7, 0, "sampling_exit"),
		asm.JEq.Imm(asm.R7, 1, "sampling_selected"),
		asm.LoadImm(asm.R8, 1<<32, asm.DWord),
		asm.Div.Reg(asm.R8, asm.R7),
		asm.FnGetPrandomU32.Call(),
		asm.JGE.Reg(asm.R0, asm.R8, "sampling_exit"),
		asm.LoadMapPtr(asm.R1, s.stats.FD()).WithSymbol("sampling_selected"),
		asm.Mov.Reg(asm.R2, asm.R10),
		asm.Add.Imm(asm.R2, -56),
		asm.FnMapLookupElem.Call(),
		asm.JEq.Imm(asm.R0, 0, "sampling_exit"),
		asm.Mov.Reg(asm.R9, asm.R0),
		asm.Mov.Imm(asm.R1, 1),
		asm.StoreXAdd(asm.R9, asm.R1, asm.DWord),
		asm.StoreMem(asm.R10, rateOffset, asm.R7, asm.Word),
	}
}

// OutputInstructions follows perf_event_output, counting its nonzero result
// before returning the passive action (XDP_PASS or socket-filter return zero).
func (*PerfSampler) OutputInstructions(action int32) asm.Instructions {
	add := asm.StoreXAdd(asm.R9, asm.R1, asm.DWord)
	add.Offset = 8
	return asm.Instructions{
		asm.JEq.Imm(asm.R0, 0, "sampling_exit"),
		asm.Mov.Imm(asm.R1, 1),
		add,
		asm.Mov.Imm(asm.R0, action).WithSymbol("sampling_exit"),
		asm.Return(),
	}
}
