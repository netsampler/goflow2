//go:build linux && !reflow_pcap && !reflow_dpdk

package afpacket

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"golang.org/x/sys/unix"
)

// Each epoch keeps its own immutable rate. The replacement starts receiving
// before the old ring drains, so downstream backpressure cannot extend the pause.
type adaptiveEpoch struct {
	driver  *driver
	ring    *packetRing
	monitor *adaptiveMonitor
}

func (e *adaptiveEpoch) close() {
	if e != nil {
		e.monitor.stop()
		if e.ring.fd < 0 {
			return
		}
		e.ring.close()
		// Report after capture has resumed (or shutdown), never while the
		// handoff waits for stop: a slow log sink must not extend the pause.
		if total := e.monitor.drops.Load(); total > e.monitor.initialDrops {
			slog.Warn("capture sampling epoch dropped packets", "network", "af_packet", "interface", e.driver.cfg.Capture.Interface, "sample_every", e.driver.cfg.Capture.SampleEvery, "dropped", total-e.monitor.initialDrops, "total_dropped", total)
		}
	}
}

func (d *driver) runAdaptive(ctx context.Context, emit func(capture.Frame) error) error {
	controller := capture.NewSamplingController(d.cfg.Capture.Sampling.Adaptive, uint32(d.cfg.Capture.SampleEvery))
	session, err := openAFPacketRing(d.cfg.Capture, d.index)
	if err != nil {
		return err
	}
	startMonitor := func(r *packetRing, drops uint64) *adaptiveMonitor {
		return startAdaptiveMonitor(r.fd, controller, time.Duration(d.cfg.Capture.Sampling.Adaptive.IntervalMS)*time.Millisecond, drops)
	}
	current := &adaptiveEpoch{driver: d, ring: session, monitor: startMonitor(session, 0)}
	var next *adaptiveEpoch
	defer func() { current.close(); next.close() }()
	var transitions, gapNS, pauseRX, pauseTX uint64
	countersComplete := true
	send := func(f capture.Frame) error {
		if transitions > 0 {
			f.Fields["capture_sampling_transitions"] = transitions
			f.Fields["capture_sampling_pause_total_ns"] = gapNS
			f.Fields["capture_sampling_pause_rx_bytes_total"] = pauseRX
			f.Fields["capture_sampling_pause_tx_bytes_total"] = pauseTX
			f.Fields["capture_sampling_pause_counters_complete"] = countersComplete
		}
		return emit(f)
	}
	for ctx.Err() == nil && !d.closed.Load() {
		rotate := func() error {
			// Preparation happens while the current socket still captures. Its monitor
			// may request a higher rate while preparation runs; freeze it after quiesce.
			cfg := d.cfg
			requested := current.monitor.next.Load()
			cfg.Capture.SampleEvery = int(requested)
			cfg.Capture.Sampling.Every = int(requested)
			prepared, err := prepareAFPacketRing(cfg.Capture, d.index)
			if err != nil {
				return err
			}
			handedOff := false
			defer func() {
				if !handedOff {
					prepared.close()
				}
			}()
			counterStart := time.Now()
			before, beforeErr := readInterfaceBytes(cfg.Capture.Interface)
			if err := current.ring.quiesce(d.index); err != nil {
				return fmt.Errorf("quiesce AF_PACKET sampling epoch: %w", err)
			}
			current.monitor.stop()
			if failure := current.monitor.failure.Load(); failure != nil {
				return failure.err
			}
			rate := controller.Rate() // stop joins the sole controller owner.
			cfg.Capture.SampleEvery, cfg.Capture.Sampling.Every = int(rate), int(rate)
			if rate != requested {
				if err := prepared.setSampling(cfg.Capture); err != nil {
					return fmt.Errorf("set replacement AF_PACKET sampler: %w", err)
				}
			}
			controller.ResetRecovery()
			if err := prepared.activate(d.index); err != nil {
				return err
			}
			resumedAt := time.Now()
			after, afterErr := readInterfaceBytes(cfg.Capture.Interface)
			counterInterval := time.Since(counterStart)
			next = &adaptiveEpoch{driver: &driver{cfg: cfg, index: d.index}, ring: prepared, monitor: startMonitor(prepared, current.monitor.drops.Load())}
			handedOff = true
			gap := resumedAt.Sub(current.ring.quiescedAt)
			gapNS += uint64(gap.Nanoseconds())
			transitions++
			attrs := []any{"interface", cfg.Capture.Interface, "previous_sample_every", current.driver.cfg.Capture.SampleEvery, "sample_every", rate, "capture_pause_ns", gap.Nanoseconds(), "capture_pause_total_ns", gapNS, "sampling_transitions", transitions, "capture_pause_counter_interval_ns", counterInterval.Nanoseconds()}
			if beforeErr == nil && afterErr == nil && after.rx >= before.rx && after.tx >= before.tx {
				rx, tx := after.rx-before.rx, after.tx-before.tx
				pauseRX += rx
				pauseTX += tx
				attrs = append(attrs, "capture_pause_counters_available", true, "capture_pause_rx_bytes", rx, "capture_pause_tx_bytes", tx)
			} else {
				countersComplete = false
				attrs = append(attrs, "capture_pause_counters_available", false)
				if beforeErr != nil {
					attrs = append(attrs, "capture_pause_counter_error", beforeErr.Error())
				} else if afterErr != nil {
					attrs = append(attrs, "capture_pause_counter_error", afterErr.Error())
				} else {
					attrs = append(attrs, "capture_pause_counter_error", "interface counters reset during transition")
				}
			}
			slog.Info("capture sampling rate changed", attrs...)
			return nil
		}
		err := d.readAdaptiveEpoch(ctx, current.driver, current.ring, current.monitor, send, rotate)
		current.monitor.stop()
		if err == nil {
			if failure := current.monitor.failure.Load(); failure != nil {
				err = failure.err
			}
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil || d.closed.Load() {
			return nil
		}
		if next == nil {
			return fmt.Errorf("AF_PACKET sampling epoch ended without replacement")
		}
		current.close()
		current, next = next, nil
	}
	return nil
}

func (d *driver) readAdaptiveEpoch(ctx context.Context, epoch *driver, s *packetRing, monitor *adaptiveMonitor, emit func(capture.Frame) error, rotate func() error) error {
	var consumed uint64
	var admitted uint64
	var drainDeadline time.Time
	for blockIndex := 0; ctx.Err() == nil && !d.closed.Load(); {
		if failure := monitor.failure.Load(); failure != nil {
			return failure.err
		}
		if drainDeadline.IsZero() && monitor.next.Load() != uint32(epoch.cfg.Capture.SampleEvery) {
			if err := rotate(); err != nil {
				return err
			}
			admitted = monitor.admitted.Load()
			drainDeadline = time.Now().Add(2*s.timeout + time.Second)
		}
		if !drainDeadline.IsZero() && consumed >= admitted {
			return nil
		}
		if !drainDeadline.IsZero() && time.Now().After(drainDeadline) {
			return fmt.Errorf("AF_PACKET sampling epoch did not drain: read %d of %d admitted records", consumed, admitted)
		}
		block := s.ring[blockIndex*s.blockSize : (blockIndex+1)*s.blockSize]
		status := (*uint32)(unsafe.Pointer(&block[8]))
		if atomic.LoadUint32(status)&unix.TP_STATUS_USER == 0 {
			if _, err := unix.Poll([]unix.PollFd{{Fd: int32(s.fd), Events: unix.POLLIN}}, 100); err != nil && err != unix.EINTR {
				return err
			}
			continue
		}
		count := binary.NativeEndian.Uint32(block[12:])
		err := epoch.readBlock(block, monitor.drops.Load(), emit)
		atomic.StoreUint32(status, unix.TP_STATUS_KERNEL)
		if err != nil {
			return err
		}
		consumed += uint64(count)
		if !drainDeadline.IsZero() {
			// Bound waiting for the kernel to retire a partial block, not
			// downstream processing time for records that are making progress.
			drainDeadline = time.Now().Add(2*s.timeout + time.Second)
		}
		blockIndex = (blockIndex + 1) % s.count
	}
	return nil
}

type adaptiveMonitor struct {
	initialDrops uint64
	stopCh       chan struct{}
	done         chan struct{}
	stopped      atomic.Bool
	next         atomic.Uint32
	drops        atomic.Uint64
	admitted     atomic.Uint64
	failure      atomic.Pointer[adaptiveMonitorFailure]
}

type adaptiveMonitorFailure struct{ err error }

// This goroutine is the only reader of PACKET_STATISTICS, whose reads reset the
// kernel counters. It remains responsive while the downstream pipeline blocks.
func startAdaptiveMonitor(fd int, controller *capture.SamplingController, interval time.Duration, initialDrops uint64) *adaptiveMonitor {
	m := &adaptiveMonitor{initialDrops: initialDrops, stopCh: make(chan struct{}), done: make(chan struct{})}
	m.drops.Store(initialDrops)
	m.next.Store(controller.Rate())
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		collect := func(adapt bool) {
			stats, err := unix.GetsockoptTpacketStatsV3(fd, unix.SOL_PACKET, unix.PACKET_STATISTICS)
			if err != nil {
				m.failure.Store(&adaptiveMonitorFailure{fmt.Errorf("read adaptive AF_PACKET statistics: %w", err)})
				return
			}
			m.drops.Add(uint64(stats.Drops))
			// Linux adds drops to a uint32 successful-admission counter. Undo
			// that addition before widening so its modular overflow is harmless.
			admitted := uint64(stats.Packets - stats.Drops)
			m.admitted.Add(admitted)
			if adapt {
				m.next.Store(controller.Observe(time.Now(), uint64(stats.Drops), admitted))
			}
		}
		for {
			select {
			case <-ticker.C:
				collect(true)
			case <-m.stopCh:
				collect(false)
				return
			}
		}
	}()
	return m
}
func (m *adaptiveMonitor) stop() {
	if m.stopped.CompareAndSwap(false, true) {
		close(m.stopCh)
	}
	<-m.done
}
