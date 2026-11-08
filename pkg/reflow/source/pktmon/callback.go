package pktmon

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

type identity struct {
	Name  string
	Index int
}

// callbackQueue receives borrowed streaming buffers. Ports are populated before
// the native stream starts and remain immutable while callbacks can execute.
type callbackQueue struct {
	cfg                            config.CaptureConfig
	done                           <-chan struct{}
	queue                          chan capture.Frame
	ports                          map[uint32]identity
	filter                         *regexp.Regexp
	rate                           atomic.Uint32
	selected                       atomic.Uint64
	readLoss, writeLoss, queueLoss atomic.Uint64
}

func newCallbackQueue(cfg config.CaptureConfig, done <-chan struct{}) *callbackQueue {
	n := cfg.Pktmon.QueueSize
	if n <= 0 {
		n = 1024
	}
	q := &callbackQueue{cfg: cfg, done: done, queue: make(chan capture.Frame, n), ports: make(map[uint32]identity)}
	if cfg.InterfaceFilter != "" {
		q.filter = regexp.MustCompile(cfg.InterfaceFilter) // NormalizeCapture validated it.
	}
	q.rate.Store(uint32(cfg.SampleEvery))
	return q
}

func (q *callbackQueue) recordMissed(read, write uint32) {
	q.readLoss.Add(uint64(read))
	q.writeLoss.Add(uint64(write))
}

func (q *callbackQueue) losses() uint64 {
	return q.readLoss.Load() + q.writeLoss.Load() + q.queueLoss.Load()
}

func (q *callbackQueue) receive(data, metadata []byte) {
	select {
	case <-q.done:
		return
	default:
	}
	direction, component := packetIdentity(metadata)
	port := q.ports[component]
	if q.cfg.Direction != "both" && q.cfg.Direction != "" && q.cfg.Direction != direction {
		return
	}
	if q.filter != nil && !q.filter.MatchString(port.Name) {
		return
	}
	var rate uint32
	if q.cfg.Sampling.Location == "callback" {
		// Read once: an adjustment after this decision must never relabel an
		// already-selected packet, including one waiting in the queue.
		rate = q.rate.Load()
		if rate > 1 && rand.Uint32N(rate) != 0 {
			return
		}
		q.selected.Add(1)
	}
	// Selection and eligibility checks precede payload copying and metadata
	// allocation. Packet Monitor itself has already delivered this observation.
	frame := decodePacket(append([]byte(nil), data...), metadata)
	frame.Interface, frame.InterfaceIndex = port.Name, port.Index
	if rate > 0 {
		frame.Sampling = &event.SamplingMetadata{Rate: rate}
	}
	frame.Drops = q.losses()
	if frame.Fields == nil {
		frame.Fields = make(map[string]any)
	}
	frame.Fields["pktmon_missed_reads"] = q.readLoss.Load()
	frame.Fields["pktmon_missed_writes"] = q.writeLoss.Load()
	frame.Fields["capture_queue_drops"] = q.queueLoss.Load()
	select {
	case <-q.done:
	case q.queue <- frame:
	default:
		q.queueLoss.Add(1)
	}
}

type callbackStats struct{ loss, selected uint64 }

// updateSampling has one owner, the monitor. Callback counters remain readable
// while Run is blocked delivering a previously queued packet to the pipeline.
func (q *callbackQueue) updateSampling(now time.Time, controller *capture.SamplingController, previous callbackStats) callbackStats {
	current := callbackStats{loss: q.losses(), selected: q.selected.Load()}
	oldRate := controller.Rate()
	newRate := controller.Observe(now, current.loss-previous.loss, current.selected-previous.selected)
	if newRate != oldRate {
		q.rate.Store(newRate)
		slog.Info("capture sampling rate changed", "network", "pktmon", "interface", q.cfg.Interface,
			"sampling_location", "callback", "previous_sample_every", oldRate,
			"sample_every", newRate, "total_dropped", current.loss)
	}
	return current
}

func (q *callbackQueue) startMonitor(ctx context.Context) (stop func()) {
	if !q.cfg.Sampling.Adaptive.Enabled {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Duration(q.cfg.Sampling.Adaptive.IntervalMS) * time.Millisecond)
		defer ticker.Stop()
		controller := capture.NewSamplingController(q.cfg.Sampling.Adaptive, uint32(q.cfg.SampleEvery))
		var previous callbackStats
		for {
			select {
			case <-ctx.Done():
				return
			case <-q.done:
				return
			case now := <-ticker.C:
				previous = q.updateSampling(now, controller, previous)
			}
		}
	}()
	return func() { cancel(); <-done }
}
