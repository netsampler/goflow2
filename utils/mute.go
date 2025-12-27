package utils

import (
	"time"
)

// BatchMute throttles events by limiting count per interval.
type BatchMute struct {
	batchTime     time.Time
	resetInterval time.Duration
	ctr           int
	max           int
}

func (b *BatchMute) increment(val int, t time.Time) (muted bool, skipped int) {

	if b.max == 0 || b.resetInterval == 0 {
		return muted, skipped
	}

	if b.ctr >= b.max {
		skipped = b.ctr - b.max
	}

	if t.Sub(b.batchTime) > b.resetInterval {
		b.ctr = 0
		b.batchTime = t
	}
	b.ctr += val

	return b.max > 0 && b.ctr > b.max, skipped
}

// Increment records a single event and reports whether muting applies.
func (b *BatchMute) Increment() (muting bool, skipped int) {
	return b.increment(1, time.Now().UTC())
}

// NewBatchMute creates a BatchMute with a reset interval and max count.
func NewBatchMute(resetInterval time.Duration, max int) *BatchMute {
	return &BatchMute{
		batchTime:     time.Now().UTC(),
		resetInterval: resetInterval,
		max:           max,
	}
}
