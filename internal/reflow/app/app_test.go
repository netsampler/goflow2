package app

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/decode"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/processor"
	"github.com/netsampler/goflow2/v3/internal/reflow/sink"
	"github.com/netsampler/goflow2/v3/internal/reflow/source"
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
		Packet: &event.PacketModel{
			Layers: []event.LayerSpec{
				{Kind: "ethernet"},
				{Kind: "mpls"},
				{Kind: "ipv4"},
			},
		},
	}

	if !aggregatorMatches(config.AggregatorConfig{
		Match: map[string]string{
			"record_kind":           "packet",
			"stream":                "agg_samples",
			"source.type":           "flow",
			"source.network":        "udp",
			"source.address":        ":18081",
			"source_id":             "7",
			"packet.has_layer.mpls": "true",
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

	if aggregatorMatches(config.AggregatorConfig{
		Match: map[string]string{
			"packet.has_layer.vxlan": "true",
		},
	}, evt) {
		t.Fatalf("expected missing layer match to fail")
	}
}

func TestRunClosesStdoutLikeSinkOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	src := &blockingSource{}
	out := &closeTrackingSink{}
	app := &App{
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		sources:          []source.Source{src},
		decoder:          noopDecoder{},
		processor:        passthroughProcessor{},
		processorWorkers: 1,
		encoderCfg:       config.EncoderConfig{Type: "json"},
		encoderWorkers:   1,
		sink:             out,
	}

	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx)
	}()

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Run did not return after context cancellation")
	}
	if got := out.closeCount.Load(); got != 1 {
		t.Fatalf("expected stdout-like sink Close to be called once, got %d", got)
	}
	if got := src.closeCount.Load(); got != 1 {
		t.Fatalf("expected source Close to be called once, got %d", got)
	}
}

type blockingSource struct {
	closeCount atomic.Int32
}

func (s *blockingSource) InitEvents() ([]*event.Event, error) {
	return nil, nil
}

func (s *blockingSource) Start(ctx context.Context, _ func(*event.Event) error) error {
	<-ctx.Done()
	return ctx.Err()
}

func (s *blockingSource) Close() error {
	s.closeCount.Add(1)
	return nil
}

type closeTrackingSink struct {
	closeCount atomic.Int32
}

func (s *closeTrackingSink) Send(_ []byte) error {
	return nil
}

func (s *closeTrackingSink) Close() error {
	s.closeCount.Add(1)
	return nil
}

type noopDecoder struct{}

func (noopDecoder) Decode(evt *event.Event) ([]*event.Event, error) {
	return []*event.Event{evt}, nil
}

func (noopDecoder) Close() {}

var _ decode.Decoder = noopDecoder{}
var _ sink.Sink = (*closeTrackingSink)(nil)
var _ source.Source = (*blockingSource)(nil)

type passthroughProcessor struct{}

func (passthroughProcessor) Process(evt *event.Event) ([]*event.Event, error) {
	return []*event.Event{evt}, nil
}

var _ processor.Processor = passthroughProcessor{}
