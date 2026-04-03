package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// StoreEntryMetrics manages reusable Prometheus series for live store entries.
type StoreEntryMetrics struct {
	entries  *prometheus.GaugeVec
	writes   *prometheus.GaugeVec
	accesses *prometheus.GaugeVec
}

// NewStoreEntryMetrics creates a helper for store entry gauges and timestamps.
func NewStoreEntryMetrics(entries *prometheus.GaugeVec, writes *prometheus.GaugeVec, accesses *prometheus.GaugeVec) StoreEntryMetrics {
	return StoreEntryMetrics{
		entries:  entries,
		writes:   writes,
		accesses: accesses,
	}
}

// OnWrite records that an entry is present and updates the write timestamp.
func (m StoreEntryMetrics) OnWrite(labels prometheus.Labels) {
	if m.entries != nil {
		m.entries.With(labels).Set(1)
	}
	if m.writes != nil {
		m.writes.With(labels).Set(float64(time.Now().Unix()))
	}
}

// OnAccess updates the access timestamp for an entry.
func (m StoreEntryMetrics) OnAccess(labels prometheus.Labels) {
	if m.accesses != nil {
		m.accesses.With(labels).Set(float64(time.Now().Unix()))
	}
}

// OnDelete removes all series associated with an entry.
func (m StoreEntryMetrics) OnDelete(labels prometheus.Labels) {
	if m.entries != nil {
		m.entries.Delete(labels)
	}
	if m.writes != nil {
		m.writes.Delete(labels)
	}
	if m.accesses != nil {
		m.accesses.Delete(labels)
	}
}
