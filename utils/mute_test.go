package utils

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBatchMute(t *testing.T) {
	start := time.Date(2023, time.November, 10, 23, 0, 0, 0, time.UTC)
	steps := []struct {
		after   time.Duration
		muted   bool
		skipped int
	}{
		{0, false, 0},
		{time.Second, false, 0},
		{2 * time.Second, true, 0},
		{10 * time.Second, true, 1},  // The interval resets only after the boundary.
		{11 * time.Second, false, 2}, // Report the previous interval's skipped events.
		{12 * time.Second, false, 0},
		{13 * time.Second, true, 0},
	}
	for _, tc := range []struct {
		name     string
		max      int
		interval time.Duration
	}{
		{"limited", 2, 10 * time.Second},
		{"zero-max", 0, 10 * time.Second},
		{"zero-interval", 2, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bm := BatchMute{batchTime: start, resetInterval: tc.interval, max: tc.max}
			for _, step := range steps {
				muted, skipped := bm.increment(1, start.Add(step.after))
				wantMuted, wantSkipped := step.muted, step.skipped
				if tc.max == 0 || tc.interval == 0 {
					wantMuted, wantSkipped = false, 0
				}
				require.Equal(t, wantMuted, muted, "muting after %s", step.after)
				require.Equal(t, wantSkipped, skipped, "skipped events after %s", step.after)
			}
		})
	}
}
