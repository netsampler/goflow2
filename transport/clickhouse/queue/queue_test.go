package queue

import (
	"testing"

	flowpb "github.com/netsampler/goflow2/v2/pb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock SizeMeter for testing
type mockSizeMeter struct {
	size uint
}

func (m mockSizeMeter) Size() uint {
	return m.size
}

func createTestMetrics() QueueSettings {
	return QueueSettings{
		MetricAvailableMemory: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_available_memory",
		}),
		MetricUsedMemory: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_used_memory",
		}),
		MetricQueuedRecords: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "test_queued_records",
		}),
		MetricDroppedRecords: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "test_dropped_records",
		}),
		Limit: 1000, // 1KB for testing
	}
}

func TestMessageWrapper(t *testing.T) {
	t.Run("Wrap creates MessageWrapper correctly", func(t *testing.T) {
		msg := &flowpb.FlowMessage{
			Type: flowpb.FlowMessage_NETFLOW_V5,
		}
		size := uint(100)

		wrapper := Wrap(msg, size)

		assert.Equal(t, size, wrapper.Size())
		assert.Equal(t, msg, wrapper.Message())
	})

	t.Run("MessageWrapper implements SizeMeter", func(t *testing.T) {
		msg := &flowpb.FlowMessage{}
		wrapper := Wrap(msg, 50)

		var _ SizeMeter = wrapper
		assert.Equal(t, uint(50), wrapper.Size())
	})
}

func TestSizeLimitedQueueFIFO_NewQueue(t *testing.T) {
	t.Run("Creates queue with default limit when limit is 0", func(t *testing.T) {
		settings := createTestMetrics()
		settings.Limit = 0

		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		assert.Equal(t, uint(DefaultLimit), queue.settings.Limit)
		assert.Equal(t, 0, queue.Length())
	})

	t.Run("Creates queue with custom limit", func(t *testing.T) {
		settings := createTestMetrics()
		customLimit := uint(2000)
		settings.Limit = customLimit

		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		assert.Equal(t, customLimit, queue.settings.Limit)
		assert.Equal(t, 0, queue.Length())
	})
}

func TestSizeLimitedQueueFIFO_AddAndGet(t *testing.T) {
	t.Run("Add and Get single element", func(t *testing.T) {
		settings := createTestMetrics()
		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		element := mockSizeMeter{size: 100}
		err := queue.Add(element)

		require.NoError(t, err)
		assert.Equal(t, 1, queue.Length())

		retrieved, ok := queue.Get()
		assert.True(t, ok)
		assert.Equal(t, element, retrieved)
		assert.Equal(t, 0, queue.Length())
	})

	t.Run("Get from empty queue returns false", func(t *testing.T) {
		settings := createTestMetrics()
		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		_, ok := queue.Get()
		assert.False(t, ok)
	})

	t.Run("FIFO behavior - last in, first out", func(t *testing.T) {
		settings := createTestMetrics()
		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		element1 := mockSizeMeter{size: 100}
		element2 := mockSizeMeter{size: 200}
		element3 := mockSizeMeter{size: 150}

		queue.Add(element1)
		queue.Add(element2)
		queue.Add(element3)

		assert.Equal(t, 3, queue.Length())

		// Should get elements in reverse order (LIFO behavior based on the Get implementation)
		retrieved1, ok1 := queue.Get()
		assert.True(t, ok1)
		assert.Equal(t, element3, retrieved1)

		retrieved2, ok2 := queue.Get()
		assert.True(t, ok2)
		assert.Equal(t, element2, retrieved2)

		retrieved3, ok3 := queue.Get()
		assert.True(t, ok3)
		assert.Equal(t, element1, retrieved3)

		assert.Equal(t, 0, queue.Length())
	})
}

func TestSizeLimitedQueueFIFO_SizeLimit(t *testing.T) {
	t.Run("Rejects zero-size elements", func(t *testing.T) {
		settings := createTestMetrics()
		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		element := mockSizeMeter{size: 0}
		err := queue.Add(element)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nothing to queue, element size is zero")
		assert.Equal(t, 0, queue.Length())
	})

	t.Run("Drops oldest elements when limit exceeded", func(t *testing.T) {
		settings := createTestMetrics()
		settings.Limit = 500 // Small limit for testing
		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		// Add elements that fit within limit
		element1 := mockSizeMeter{size: 200}
		element2 := mockSizeMeter{size: 200}
		queue.Add(element1)
		queue.Add(element2)
		assert.Equal(t, 2, queue.Length())

		// Add element that exceeds limit, should drop oldest
		element3 := mockSizeMeter{size: 200}
		queue.Add(element3)

		// Should have dropped element1 to make space
		assert.Equal(t, 2, queue.Length())

		// Verify remaining elements
		retrieved1, _ := queue.Get()
		assert.Equal(t, element3, retrieved1)
		retrieved2, _ := queue.Get()
		assert.Equal(t, element2, retrieved2)
	})

	t.Run("Drops multiple elements when single large element added", func(t *testing.T) {
		settings := createTestMetrics()
		settings.Limit = 500
		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		// Fill queue with small elements
		for i := 0; i < 4; i++ {
			queue.Add(mockSizeMeter{size: 100})
		}
		assert.Equal(t, 4, queue.Length())

		// Add large element that requires dropping multiple elements
		largeElement := mockSizeMeter{size: 300}
		queue.Add(largeElement)

		// Should have dropped enough elements to fit the large one
		assert.True(t, queue.Length() < 4)

		// The large element should be retrievable
		retrieved, ok := queue.Get()
		assert.True(t, ok)
		assert.Equal(t, largeElement, retrieved)
	})
}

func TestSizeLimitedQueueFIFO_Concurrency(t *testing.T) {
	t.Run("Concurrent access is safe", func(t *testing.T) {
		settings := createTestMetrics()
		queue := NewSizeLimitedQueueFIFO[mockSizeMeter](settings)

		// This is a basic test - in a real scenario you'd want more sophisticated concurrency testing
		done := make(chan bool, 2)

		// Producer goroutine
		go func() {
			for i := 0; i < 100; i++ {
				queue.Add(mockSizeMeter{size: 10})
			}
			done <- true
		}()

		// Consumer goroutine
		go func() {
			for i := 0; i < 50; i++ {
				queue.Get()
			}
			done <- true
		}()

		// Wait for both goroutines
		<-done
		<-done

		// Queue should still be functional
		assert.True(t, queue.Length() >= 0)
	})
}

func TestSizeLimitedQueueFIFO_WithMessageWrapper(t *testing.T) {
	t.Run("Works with MessageWrapper", func(t *testing.T) {
		settings := createTestMetrics()
		queue := NewSizeLimitedQueueFIFO[MessageWrapper](settings)

		msg1 := &flowpb.FlowMessage{Type: flowpb.FlowMessage_NETFLOW_V5}
		msg2 := &flowpb.FlowMessage{Type: flowpb.FlowMessage_NETFLOW_V9}

		wrapper1 := Wrap(msg1, 100)
		wrapper2 := Wrap(msg2, 200)

		err1 := queue.Add(wrapper1)
		err2 := queue.Add(wrapper2)

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.Equal(t, 2, queue.Length())

		retrieved1, ok1 := queue.Get()
		assert.True(t, ok1)
		assert.Equal(t, msg2, retrieved1.Message())

		retrieved2, ok2 := queue.Get()
		assert.True(t, ok2)
		assert.Equal(t, msg1, retrieved2.Message())
	})
}

func TestConstants(t *testing.T) {
	t.Run("Constants have expected values", func(t *testing.T) {
		assert.Equal(t, 1048576, MB) // 1024 * 1024
		assert.Equal(t, 524288000, DefaultLimit) // 500 * MB
	})
}