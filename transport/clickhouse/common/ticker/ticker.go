package ticker

import (
	"context"
	"time"
)

func Run(ctx context.Context, updatePeriod, forceUpdate time.Duration, update func(context.Context) error) error {
	updateTicker := time.NewTicker(updatePeriod)
	forceUpdateTicker := time.NewTicker(forceUpdate)
	defer updateTicker.Stop()
	defer forceUpdateTicker.Stop()

	force := false
	updateAndSetForce := func() {
		if err := update(ctx); err != nil {
			force = true
		} else {
			force = false
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-updateTicker.C:
			if !force {
				updateAndSetForce()
			}
		case <-forceUpdateTicker.C:
			if force {
				updateAndSetForce()
			}

		}
	}
}
