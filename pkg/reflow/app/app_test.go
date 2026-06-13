package app

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/decode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/processor"
	"github.com/netsampler/goflow2/v3/pkg/reflow/sink"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source"
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

	sourceInit := &event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type:   "source_init",
			Stream: "options_data",
		},
		Source: event.SourceMetadata{
			SourceID:    7,
			SourceIDSet: true,
			Sampling: &event.SamplingMetadata{
				Rate: 100,
			},
		},
	}
	if !aggregatorMatches(config.AggregatorConfig{
		Match: map[string]string{
			"kind":                 "control",
			"control.type":         "source_init",
			"control.stream":       "options_data",
			"source.source_id":     "7",
			"source.sampling.rate": "100",
		},
	}, sourceInit) {
		t.Fatalf("expected source_init control match to succeed")
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

func TestRunRefreshesSourceInitEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &refreshingSource{}
	out := &countingSink{
		after:  2,
		cancel: cancel,
	}
	app := &App{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		sources:           []source.Source{src},
		sourceInitRefresh: []time.Duration{10 * time.Millisecond},
		decoder:           noopDecoder{},
		processor:         passthroughProcessor{},
		processorWorkers:  1,
		encoderCfg:        config.EncoderConfig{Type: "json"},
		encoderWorkers:    1,
		sink:              out,
	}

	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Run did not refresh source init events")
	}
	if got := src.initCount.Load(); got < 2 {
		t.Fatalf("expected source InitEvents to be called at least twice, got %d", got)
	}
	if got := out.sendCount.Load(); got < 2 {
		t.Fatalf("expected at least two encoded source init events, got %d", got)
	}
}

func TestRunRefreshesSourceInitEventsAfterEmptyInitialSet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &refreshingSource{emptyFirst: true}
	out := &countingSink{
		after:  1,
		cancel: cancel,
	}
	app := &App{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		sources:           []source.Source{src},
		sourceInitRefresh: []time.Duration{10 * time.Millisecond},
		decoder:           noopDecoder{},
		processor:         passthroughProcessor{},
		processorWorkers:  1,
		encoderCfg:        config.EncoderConfig{Type: "json"},
		encoderWorkers:    1,
		sink:              out,
	}

	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("Run did not refresh source init events after an empty initial set")
	}
	if got := src.initCount.Load(); got < 2 {
		t.Fatalf("expected source InitEvents to be retried, got %d calls", got)
	}
	if got := out.sendCount.Load(); got != 1 {
		t.Fatalf("expected one encoded refreshed source init event, got %d", got)
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

type refreshingSource struct {
	initCount  atomic.Int32
	closeCount atomic.Int32
	emptyFirst bool
}

func (s *refreshingSource) InitEvents() ([]*event.Event, error) {
	count := s.initCount.Add(1)
	if s.emptyFirst && count == 1 {
		return nil, nil
	}
	return []*event.Event{
		{
			ReceivedAt: time.Now().UTC(),
			Kind:       "control",
			Control: &event.ControlMetadata{
				Type:   "source_init",
				Stream: "options_data",
			},
			Fields: map[string]any{
				"init_count": count,
			},
		},
	}, nil
}

func (s *refreshingSource) Start(ctx context.Context, _ func(*event.Event) error) error {
	<-ctx.Done()
	return ctx.Err()
}

func (s *refreshingSource) Close() error {
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

type countingSink struct {
	sendCount atomic.Int32
	after     int32
	cancel    context.CancelFunc
}

func (s *countingSink) Send(_ []byte) error {
	count := s.sendCount.Add(1)
	if s.cancel != nil && s.after > 0 && count >= s.after {
		s.cancel()
	}
	return nil
}

func (s *countingSink) Close() error {
	return nil
}

type noopDecoder struct{}

func (noopDecoder) Decode(evt *event.Event) ([]*event.Event, error) {
	return []*event.Event{evt}, nil
}

func (noopDecoder) Errors() <-chan error { return nil }

func (noopDecoder) Close() {}

var _ decode.Decoder = noopDecoder{}
var _ sink.Sink = (*closeTrackingSink)(nil)
var _ sink.Sink = (*countingSink)(nil)
var _ source.Source = (*blockingSource)(nil)
var _ source.Source = (*refreshingSource)(nil)

type passthroughProcessor struct{}

func (passthroughProcessor) Process(evt *event.Event) ([]*event.Event, error) {
	return []*event.Event{evt}, nil
}

var _ processor.Processor = passthroughProcessor{}
