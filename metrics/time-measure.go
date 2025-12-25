package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type TimeMeasure struct {
	now time.Time
}

func TimeMeasureNow() TimeMeasure {
	return TimeMeasure{now: time.Now()}
}
func (t TimeMeasure) MeasureTime(metric prometheus.Gauge) {
	metric.Set(float64(time.Since(t.now).Milliseconds()))
}
