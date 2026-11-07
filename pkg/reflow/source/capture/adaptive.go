package capture

import (
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

// SamplingController has one owner: a backend's independent statistics monitor.
// Its inputs are interval delivery losses and selected packet activity, never
// intentionally excluded packets or unrelated interface errors.
type SamplingController struct {
	cfg        config.AdaptiveSamplingConfig
	rate       uint32
	quietSince time.Time
}

func NewSamplingController(cfg config.AdaptiveSamplingConfig, rate uint32) *SamplingController {
	return &SamplingController{cfg: cfg, rate: rate}
}

func (c *SamplingController) Rate() uint32   { return c.rate }
func (c *SamplingController) ResetRecovery() { c.quietSince = time.Time{} }

func (c *SamplingController) Observe(now time.Time, drops, packets uint64) uint32 {
	if !c.cfg.Enabled {
		return c.rate
	}
	if drops > c.cfg.DropThreshold {
		c.quietSince = now
		next := min(uint64(c.rate)*uint64(c.cfg.IncreaseFactor), uint64(c.cfg.MaxEvery))
		c.rate = uint32(next)
		return c.rate
	}
	if packets == 0 {
		c.ResetRecovery()
		return c.rate
	}
	if c.quietSince.IsZero() {
		c.quietSince = now
		return c.rate
	}
	if now.Sub(c.quietSince) >= time.Duration(c.cfg.RecoveryAfterMS)*time.Millisecond {
		c.rate = max(c.rate/uint32(c.cfg.DecreaseFactor), uint32(c.cfg.MinEvery))
		c.quietSince = now
	}
	return c.rate
}
