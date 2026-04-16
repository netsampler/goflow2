package aggregate

import (
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func TestStatefulFlushSumsPacketCounters(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	first := &event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(60),
			"packets":  int64(1),
		},
	}
	second := &event.Event{
		ReceivedAt: time.Unix(2, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(70),
			"packets":  int64(1),
		},
	}

	if out, err := agg.Process(first); err != nil || len(out) != 0 {
		t.Fatalf("first Process returned out=%d err=%v", len(out), err)
	}
	if out, err := agg.Process(second); err != nil || len(out) != 0 {
		t.Fatalf("second Process returned out=%d err=%v", len(out), err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if got := out[0].Fields["packets"]; got != int64(2) {
		t.Fatalf("expected packets=2, got %#v", got)
	}
	if got := out[0].Fields["bytes"]; got != int64(130) {
		t.Fatalf("expected bytes=130, got %#v", got)
	}
}

func TestStatefulInitEventsCarryConfiguredStreamAndTemplateBaseID(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Stream:  "agg_packets",
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		TemplateID: 512,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	events, err := agg.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 init event, got %d", len(events))
	}
	if events[0].Stream != "agg_packets" {
		t.Fatalf("expected event stream agg_packets, got %q", events[0].Stream)
	}
	if events[0].Control == nil || events[0].Control.Stream != "agg_packets" {
		t.Fatalf("expected control stream agg_packets, got %#v", events[0].Control)
	}
	schema, ok := events[0].Payload.(event.AggregationSchema)
	if !ok {
		t.Fatalf("expected aggregation schema payload, got %T", events[0].Payload)
	}
	if schema.Stream != "agg_packets" {
		t.Fatalf("expected schema stream agg_packets, got %q", schema.Stream)
	}
	if schema.BaseTemplateID != 512 {
		t.Fatalf("expected base template id 512, got %d", schema.BaseTemplateID)
	}
}

func TestStatefulInitEventsSortStaticFieldsDeterministically(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Stream:  "agg_packets",
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr"},
		Sum:       []string{"bytes"},
		StaticFields: map[string]any{
			"z_field": "z",
			"a_field": "a",
			"m_field": "m",
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	events, err := agg.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	schema, ok := events[0].Payload.(event.AggregationSchema)
	if !ok {
		t.Fatalf("expected aggregation schema payload, got %T", events[0].Payload)
	}
	want := []string{"src_addr", "bytes", "start_time_unix", "end_time_unix", "a_field", "m_field", "z_field"}
	if len(schema.FieldNames) != len(want) {
		t.Fatalf("expected %d field names, got %#v", len(want), schema.FieldNames)
	}
	for i, field := range want {
		if schema.FieldNames[i] != field {
			t.Fatalf("expected field_names[%d]=%q, got %#v", i, field, schema.FieldNames)
		}
	}
}

func TestStatefulAggregatedEventsCarryConfiguredStream(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Stream:  "agg_counters",
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if _, err := agg.Process(&event.Event{
		Fields: map[string]any{
			"if_in_octets": int64(64),
		},
	}); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if out[0].Stream != "agg_counters" {
		t.Fatalf("expected stream agg_counters, got %q", out[0].Stream)
	}
}

func TestStatefulTTLFlushSumsPacketCounters(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Window: config.AggregatorWindowConfig{
			IdleFlushAfter: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	first := &event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(60),
			"packets":  int64(1),
		},
	}
	second := &event.Event{
		ReceivedAt: time.Unix(2, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(70),
			"packets":  int64(1),
		},
	}

	if out, err := agg.Process(first); err != nil || len(out) != 0 {
		t.Fatalf("first Process returned out=%d err=%v", len(out), err)
	}
	if out, err := agg.Process(second); err != nil || len(out) != 0 {
		t.Fatalf("second Process returned out=%d err=%v", len(out), err)
	}

	time.Sleep(10 * time.Millisecond)

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if got := out[0].Fields["packets"]; got != int64(2) {
		t.Fatalf("expected packets=2, got %#v", got)
	}
	if got := out[0].Fields["bytes"]; got != int64(130) {
		t.Fatalf("expected bytes=130, got %#v", got)
	}
}

func TestStatefulTracksMinStartAndMaxEndTimestamps(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(10, 0),
		Fields: map[string]any{
			"src_addr":        "192.0.2.1",
			"dst_addr":        "198.51.100.2",
			"proto":           uint32(6),
			"src_port":        uint32(12345),
			"dst_port":        uint32(443),
			"bytes":           int64(60),
			"packets":         int64(1),
			"start_time_unix": int64(5_000),
			"end_time_unix":   int64(8_000),
		},
	})
	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(11, 0),
		Fields: map[string]any{
			"src_addr":        "192.0.2.1",
			"dst_addr":        "198.51.100.2",
			"proto":           uint32(6),
			"src_port":        uint32(12345),
			"dst_port":        uint32(443),
			"bytes":           int64(70),
			"packets":         int64(1),
			"start_time_unix": int64(4_000),
			"end_time_unix":   int64(9_000),
		},
	})

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := out[0].Fields["start_time_unix"]; got != int64(4_000) {
		t.Fatalf("expected start_time_unix=4000, got %#v", got)
	}
	if got := out[0].Fields["end_time_unix"]; got != int64(9_000) {
		t.Fatalf("expected end_time_unix=9000, got %#v", got)
	}
}

func TestStatefulOnlySumsConfiguredSumFields(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(60),
			"packets":  int64(1),
		},
	})
	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(2, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(70),
			"packets":  int64(1),
		},
	})

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := out[0].Fields["proto"]; got != uint32(6) {
		t.Fatalf("expected proto=6 to remain stable, got %#v", got)
	}
	if got := out[0].Fields["bytes"]; got != int64(130) {
		t.Fatalf("expected bytes=130, got %#v", got)
	}
	if got := out[0].Fields["packets"]; got != int64(2) {
		t.Fatalf("expected packets=2, got %#v", got)
	}
}

func TestStatefulPeriodicFlushOnlyEmitsDirtyBuckets(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(60),
			"packets":  int64(1),
		},
	})

	time.Sleep(10 * time.Millisecond)

	firstFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("first Flush returned error: %v", err)
	}
	if len(firstFlush) != 1 {
		t.Fatalf("expected first flush to emit 1 event, got %d", len(firstFlush))
	}

	secondFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("second Flush returned error: %v", err)
	}
	if len(secondFlush) != 0 {
		t.Fatalf("expected second flush to emit 0 events, got %d", len(secondFlush))
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(2, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(40),
			"packets":  int64(1),
		},
	})

	time.Sleep(10 * time.Millisecond)

	thirdFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("third Flush returned error: %v", err)
	}
	if len(thirdFlush) != 1 {
		t.Fatalf("expected third flush to emit 1 event, got %d", len(thirdFlush))
	}
	if got := thirdFlush[0].Fields["bytes"]; got != int64(100) {
		t.Fatalf("expected bytes=100 after update, got %#v", got)
	}
}

func TestStatefulIdleEraseDropsUntouchedBucketWithoutEmit(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Window: config.AggregatorWindowConfig{
			IdleEraseAfter: 1,
			MaxFlushAfter:  1000,
		},
		KeyFields: []string{"src_addr"},
		Sum:       []string{"bytes"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"bytes":    int64(60),
		},
	})

	time.Sleep(10 * time.Millisecond)

	out, err := agg.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected idle erase to drop bucket without emit, got %d events", len(out))
	}
}

func TestStatefulPeriodicResetDeletesBucketsAfterEmit(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled: true,
		Periodic: config.AggregatorPeriodicConfig{
			Every:        1,
			ResetBuckets: true,
		},
		KeyFields: []string{"src_addr"},
		Sum:       []string{"bytes"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"bytes":    int64(60),
		},
	})

	time.Sleep(10 * time.Millisecond)

	firstFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("first Flush returned error: %v", err)
	}
	if len(firstFlush) != 1 {
		t.Fatalf("expected first flush to emit 1 event, got %d", len(firstFlush))
	}

	secondFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("second Flush returned error: %v", err)
	}
	if len(secondFlush) != 0 {
		t.Fatalf("expected second flush to emit 0 events after reset, got %d", len(secondFlush))
	}
}
