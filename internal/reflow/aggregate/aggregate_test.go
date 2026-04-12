package aggregate

import (
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func TestStatefulFlushSumsPacketCounters(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled:   true,
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

	out, err := agg.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
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

func TestStatefulTTLFlushSumsPacketCounters(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Enabled:       true,
		ResetInterval: 1,
		KeyFields:     []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:           []string{"bytes", "packets"},
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
