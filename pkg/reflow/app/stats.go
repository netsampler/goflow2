package app

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

// StatsOptions configures the statistics collected by NewWithStats.
type StatsOptions struct {
	// TimingEvery is the average number of calls per duration observation.
	// Zero disables timing. A positive value always times flush calls.
	TimingEvery uint64
}

// Stats is a snapshot of cumulative pipeline counters and current queue gauges.
type Stats struct {
	Sources          []SourceStats // In configuration order.
	DecodedEvents    int64
	AggregateRecords int64
	SinkPayloads     int64
	SinkBytes        int64 // Encoded payload bytes, excluding sink framing.
	Errors           map[StageOperation]int64
	Durations        map[StageOperation]DurationStats
	Queues           []QueueStats
}

// SourceStats counts admitted events and capture losses reported by a source.
type SourceStats struct {
	Events       int64
	CaptureDrops int64
}

// StageOperation identifies an operation within a pipeline stage.
type StageOperation struct {
	Stage     string
	Operation string
}

// DurationStats holds total elapsed time and the number of timed calls.
type DurationStats struct {
	Total time.Duration
	Count int64
}

// QueueStats describes the current occupancy of a pipeline queue.
type QueueStats struct {
	Name            string
	AggregatorIndex int // Index in the configuration, or -1 for a shared queue.
	Depth           int
	Capacity        int
}

type sourceCounters struct {
	events       atomic.Int64
	captureDrops atomic.Int64
}

type pipelineStats struct {
	timingEvery      uint64
	sources          []sourceCounters
	decodedEvents    atomic.Int64
	aggregateRecords atomic.Int64
	sinkPayloads     atomic.Int64
	sinkBytes        atomic.Int64

	mu     sync.Mutex
	errors map[StageOperation]int64
	timers map[StageOperation][]*stageTimer
	queues []pipelineQueue
}

type pipelineQueue struct {
	name            string
	aggregatorIndex int
	jobs            <-chan *event.Event
}

func newPipelineStats(sources int, options StatsOptions) *pipelineStats {
	return &pipelineStats{
		timingEvery: options.TimingEvery,
		sources:     make([]sourceCounters, sources),
		errors:      make(map[StageOperation]int64),
		timers:      make(map[StageOperation][]*stageTimer),
	}
}

// Stats returns a snapshot that can be read during or after Run. Each duration
// sum and count are read together. New returns an app with statistics disabled.
func (a *App) Stats() Stats {
	s := a.stats
	if s == nil {
		return Stats{}
	}
	out := Stats{
		Sources:          make([]SourceStats, len(s.sources)),
		DecodedEvents:    s.decodedEvents.Load(),
		AggregateRecords: s.aggregateRecords.Load(),
		SinkPayloads:     s.sinkPayloads.Load(),
		SinkBytes:        s.sinkBytes.Load(),
	}
	for i := range s.sources {
		out.Sources[i] = SourceStats{
			Events:       s.sources[i].events.Load(),
			CaptureDrops: s.sources[i].captureDrops.Load(),
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errors) > 0 {
		out.Errors = make(map[StageOperation]int64, len(s.errors))
		for operation, count := range s.errors {
			out.Errors[operation] = count
		}
	}
	if len(s.timers) > 0 {
		out.Durations = make(map[StageOperation]DurationStats, len(s.timers))
		for operation, timers := range s.timers {
			var sum DurationStats
			for _, timer := range timers {
				timer.mu.Lock()
				sum.Count += timer.duration.Count
				sum.Total += timer.duration.Total
				timer.mu.Unlock()
			}
			if sum.Count > 0 {
				out.Durations[operation] = sum
			}
		}
	}
	for _, queue := range s.queues {
		out.Queues = append(out.Queues, QueueStats{
			Name: queue.name, AggregatorIndex: queue.aggregatorIndex,
			Depth: len(queue.jobs), Capacity: cap(queue.jobs),
		})
	}
	return out
}

func (s *pipelineStats) recordError(stage, operation string) {
	if s != nil {
		s.mu.Lock()
		s.errors[StageOperation{stage, operation}]++
		s.mu.Unlock()
	}
}

func (s *pipelineStats) addQueue(name string, aggregatorIndex int, jobs <-chan *event.Event) {
	if s != nil {
		s.mu.Lock()
		s.queues = append(s.queues, pipelineQueue{name, aggregatorIndex, jobs})
		s.mu.Unlock()
	}
}

type stageTimer struct {
	every    uint64
	mu       sync.Mutex
	duration DurationStats
}

func (s *pipelineStats) timer(stage, operation string) *stageTimer {
	if s == nil || s.timingEvery == 0 {
		return nil
	}
	every := s.timingEvery
	if operation == "flush" {
		every = 1
	}
	t := &stageTimer{every: every}
	key := StageOperation{stage, operation}
	s.mu.Lock()
	s.timers[key] = append(s.timers[key], t)
	s.mu.Unlock()
	return t
}

func (t *stageTimer) start() time.Time {
	if t == nil || t.every > 1 && rand.Uint64N(t.every) != 0 {
		return time.Time{}
	}
	return time.Now()
}

func (t *stageTimer) observe(start time.Time) {
	if !start.IsZero() {
		elapsed := time.Since(start)
		t.mu.Lock()
		t.duration.Total += elapsed
		t.duration.Count++
		t.mu.Unlock()
	}
}
