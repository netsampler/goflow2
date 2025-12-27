package queue

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	MB           = 1024 * 1024
	DefaultLimit = 500 * MB
)

type SizeMeter interface {
	Size() uint
}

type QueueSettings struct {
	MetricAvailableMemory prometheus.Gauge
	MetricUsedMemory      prometheus.Gauge
	MetricQueuedRecords   prometheus.Gauge
	MetricDroppedRecords  prometheus.Counter
	Limit                 uint
}

type SizeLimitedQueueFIFO[T SizeMeter] struct {
	settings QueueSettings

	m      sync.Mutex
	used   uint
	buffer []T
}

func NewSizeLimitedQueueFIFO[T SizeMeter](s QueueSettings) *SizeLimitedQueueFIFO[T] {
	r := SizeLimitedQueueFIFO[T]{
		settings: s,
	}
	if r.settings.Limit == 0 {
		r.settings.Limit = DefaultLimit
	}
	r.settings.MetricAvailableMemory.Set(float64(r.settings.Limit))
	return &r
}

func (r *SizeLimitedQueueFIFO[T]) Length() int {
	r.m.Lock()
	defer r.m.Unlock()
	return len(r.buffer)
}

func (r *SizeLimitedQueueFIFO[T]) Add(data T) error {
	r.m.Lock()
	defer r.m.Unlock()

	dataSize := data.Size()
	if dataSize <= 0 {
		return fmt.Errorf("nothing to queue, element size is zero")
	}

	r.allocateSpace(dataSize)

	r.buffer = append(r.buffer, data)
	r.used += dataSize

	r.settings.MetricQueuedRecords.Inc()
	r.settings.MetricUsedMemory.Set(float64(r.used))

	return nil
}

// return false if no data in queue
func (r *SizeLimitedQueueFIFO[T]) Get() (T, bool) {
	r.m.Lock()
	defer r.m.Unlock()

	var response T
	if len(r.buffer) == 0 {
		return response, false
	}

	response = r.buffer[len(r.buffer)-1]
	r.buffer = r.buffer[:len(r.buffer)-1]
	r.used -= response.Size()

	r.settings.MetricUsedMemory.Set(float64(r.used))
	r.settings.MetricQueuedRecords.Dec()
	return response, true
}

func (r *SizeLimitedQueueFIFO[T]) allocateSpace(size uint) {
	for len(r.buffer) > 0 && r.used+size > r.settings.Limit {
		oldestElement := r.buffer[0]
		tailSize := oldestElement.Size()
		r.buffer = r.buffer[1:]
		r.used -= tailSize

		r.settings.MetricDroppedRecords.Inc()
		r.settings.MetricUsedMemory.Set(float64(r.used))
		r.settings.MetricQueuedRecords.Dec()
	}
}
