package app

import (
	"testing"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func TestAggregatorMatchesByFieldsAndMetadata(t *testing.T) {
	evt := &event.Event{
		Kind:   "data",
		Stream: "agg_samples",
		Source: event.SourceMetadata{
			Type:    "flow",
			Network: "udp",
			Address: ":18081",
		},
		Fields: map[string]any{
			"record_kind": "packet",
			"source_id":   uint32(7),
		},
	}

	if !aggregatorMatches(config.AggregatorConfig{
		Match: map[string]string{
			"record_kind":    "packet",
			"stream":         "agg_samples",
			"source.type":    "flow",
			"source.network": "udp",
			"source.address": ":18081",
			"source_id":      "7",
		},
	}, evt) {
		t.Fatalf("expected match to succeed")
	}

	if aggregatorMatches(config.AggregatorConfig{
		Match: map[string]string{
			"record_kind": "interface_counter",
		},
	}, evt) {
		t.Fatalf("expected match to fail")
	}
}
