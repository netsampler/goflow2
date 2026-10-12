package aggregate

import (
	"fmt"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func BenchmarkStatefulProcess(b *testing.B) {
	for _, buckets := range []int{1, 10000} {
		b.Run(fmt.Sprintf("%d_buckets", buckets), func(b *testing.B) {
			agg, err := New(benchAggregateConfig())
			if err != nil {
				b.Fatalf("New returned error: %v", err)
			}
			// Keep packet construction and string formatting outside the measured
			// loop, and use a fixed working set independent of benchmark duration.
			events := make([]*event.Event, buckets)
			for i := range events {
				events[i] = benchAggregateEvent(
					fmt.Sprintf("192.0.%d.%d", i/250, i%250),
					"198.51.100.2", uint32(1024+i),
				)
				if _, err := agg.Process(events[i]); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := agg.Process(events[i%buckets]); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if out, err := agg.Close(); err != nil || len(out) != buckets {
				b.Fatalf("Close: events=%d want=%d err=%v", len(out), buckets, err)
			}
		})
	}
}

func BenchmarkStatefulClose(b *testing.B) {
	benchmarks := []struct {
		name    string
		buckets int
	}{
		{name: "100_buckets", buckets: 100},
		{name: "10000_buckets", buckets: 10000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			b.StopTimer()
			for i := 0; i < b.N; i++ {
				agg, err := New(benchAggregateConfig())
				if err != nil {
					b.Fatalf("New returned error: %v", err)
				}
				for bucket := 0; bucket < bm.buckets; bucket++ {
					_, err := agg.Process(benchAggregateEvent(
						fmt.Sprintf("192.0.%d.%d", bucket/250, bucket%250),
						fmt.Sprintf("198.51.%d.%d", bucket/250, bucket%250),
						uint32(1024+bucket),
					))
					if err != nil {
						b.Fatalf("Process returned error: %v", err)
					}
				}
				b.StartTimer()
				out, err := agg.Close()
				b.StopTimer()
				if err != nil {
					b.Fatalf("Close returned error: %v", err)
				}
				if len(out) != bm.buckets {
					b.Fatalf("expected %d output events, got %d", bm.buckets, len(out))
				}
			}
		})
	}
}

func benchAggregateConfig() config.AggregatorConfig {
	return config.AggregatorConfig{
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
		First:     []string{"agent_ip", "input_if"},
		Current:   []string{"output_if"},
		Min:       []string{"start_time_unix"},
		Max:       []string{"end_time_unix"},
	}
}

func benchAggregateEvent(srcAddr, dstAddr string, srcPort uint32) *event.Event {
	return &event.Event{
		ReceivedAt: time.Unix(1_700_000_001, 0).UTC(),
		Fields: map[string]any{
			"agent_ip":        "192.0.2.1",
			"src_addr":        srcAddr,
			"dst_addr":        dstAddr,
			"proto":           uint32(6),
			"src_port":        srcPort,
			"dst_port":        uint32(443),
			"bytes":           int64(321),
			"packets":         int64(7),
			"input_if":        uint32(9),
			"output_if":       uint32(10),
			"start_time_unix": int64(1_700_000_000_100),
			"end_time_unix":   int64(1_700_000_000_900),
		},
	}
}
