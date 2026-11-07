package capture

import (
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

func TestAdaptiveSamplingOverloadAndRecovery(t *testing.T) {
	start := time.Unix(1700000000, 0)
	c := NewSamplingController(config.AdaptiveSamplingConfig{Enabled: true, MinEvery: 1, MaxEvery: 32, IncreaseFactor: 4, DecreaseFactor: 2, RecoveryAfterMS: 30000}, 1)
	steps := []struct {
		seconds        int
		drops, packets uint64
		want           uint32
	}{
		{0, 1, 1000, 4},
		{1, 30, 1000, 16},
		{2, 40, 1000, 32},
		{3, 0, 1000, 32},
		{31, 0, 1000, 32},
		{32, 0, 1000, 16},
		{62, 0, 1000, 8},
		{92, 0, 0, 8}, // idle time must not masquerade as successful load
		{122, 0, 1000, 8},
		{152, 0, 1000, 4},
		{182, 0, 1000, 2},
		{212, 0, 1000, 1},
		{242, 0, 1000, 1},
	}
	for _, step := range steps {
		if got := c.Observe(start.Add(time.Duration(step.seconds)*time.Second), step.drops, step.packets); got != step.want {
			t.Fatalf("at %ds: divisor %d, want %d", step.seconds, got, step.want)
		}
	}
}
