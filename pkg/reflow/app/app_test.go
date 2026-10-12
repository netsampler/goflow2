package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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

func TestAggregatorMatchesIPAddressVersion(t *testing.T) {
	evt := &event.Event{
		Kind: "data",
		Fields: map[string]any{
			"src_addr":     "2001:db8::1",
			"nat_src_addr": "192.0.2.10",
		},
	}

	if !aggregatorMatches(config.AggregatorConfig{
		Match: map[string]string{
			"src_addr.ip_version":     "ipv6",
			"nat_src_addr.ip_version": "ipv4",
		},
	}, evt) {
		t.Fatalf("expected IP version match to succeed")
	}
	if aggregatorMatches(config.AggregatorConfig{
		Match: map[string]string{
			"src_addr.ip_version": "ipv4",
		},
	}, evt) {
		t.Fatalf("expected wrong IP version match to fail")
	}
	if aggregatorMatches(config.AggregatorConfig{
		Match: map[string]string{
			"missing_addr.ip_version": "ipv6",
		},
	}, evt) {
		t.Fatalf("expected missing IP version match to fail")
	}
}

func TestAggregatorMatchesMissingKeysOnlyWhenAggregateMissingEnabled(t *testing.T) {
	evt := &event.Event{
		Kind: "data",
		Fields: map[string]any{
			"dst_addr": "198.51.100.2",
			"bytes":    int64(60),
		},
	}

	first := config.AggregatorConfig{
		KeyFields: []string{"src_addr", "dst_addr"},
		Sum:       []string{"bytes"},
	}
	if aggregatorMatches(first, evt) {
		t.Fatalf("expected aggregator without aggregate_missing to reject missing src_addr")
	}

	next := config.AggregatorConfig{
		AggregateMissing: true,
		KeyFields:        []string{"src_addr", "dst_addr"},
		Sum:              []string{"bytes"},
	}
	if !aggregatorMatches(next, evt) {
		t.Fatalf("expected aggregator with aggregate_missing to accept missing src_addr")
	}
}

func TestMatchingAggregateWorkersWarnsOnlyWhenNoAggregatorAcceptsMissingKeys(t *testing.T) {
	evt := &event.Event{
		Kind: "data",
		Fields: map[string]any{
			"dst_addr": "198.51.100.2",
			"bytes":    int64(60),
		},
	}
	withoutAggregateMissing := aggregateWorker{
		cfg: config.AggregatorConfig{
			Stream:    "strict_flow_data",
			KeyFields: []string{"src_addr", "dst_addr"},
			Sum:       []string{"bytes"},
		},
	}
	withAggregateMissing := aggregateWorker{
		cfg: config.AggregatorConfig{
			Stream:           "missing_flow_data",
			AggregateMissing: true,
			KeyFields:        []string{"src_addr", "dst_addr"},
			Sum:              []string{"bytes"},
		},
	}

	matched, stream, key := matchingAggregateWorkers([]aggregateWorker{
		withoutAggregateMissing,
		withAggregateMissing,
	}, evt)
	if len(matched) != 1 || matched[0].cfg.Stream != "missing_flow_data" {
		t.Fatalf("expected only aggregate_missing worker to match, got %#v", matched)
	}
	if stream != "" || key != "" {
		t.Fatalf("did not expect warning details when an aggregator matched, got stream=%q key=%q", stream, key)
	}

	matched, stream, key = matchingAggregateWorkers([]aggregateWorker{withoutAggregateMissing}, evt)
	if len(matched) != 0 {
		t.Fatalf("expected no workers to match, got %#v", matched)
	}
	if stream != "strict_flow_data" || key != "src_addr" {
		t.Fatalf("expected warning details for unaggregated event, got stream=%q key=%q", stream, key)
	}
}

func TestRunClosesSourcesAndSinkOnContextCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flows.json")
	if err := os.WriteFile(path, []byte("{\"packets\":1}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []config.SourceConfig{
		{Network: "udp", Address: "127.0.0.1:0", Type: "flow"},
		{Network: "stream", Address: path, Type: "json"},
	} {
		t.Run(cfg.Network, func(t *testing.T) {
			for range 20 {
				ctx, cancel := context.WithCancel(context.Background())
				realSource, err := source.New(cfg)
				if err != nil {
					t.Fatal(err)
				}
				src := &closeTrackingSource{Source: realSource}
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
				go func() { done <- app.Run(ctx) }()
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("Run returned error: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("Run did not return after context cancellation")
				}
				if out.closeCount.Load() != 1 || src.closeCount.Load() != 1 {
					t.Fatal("Run must close both the source and sink once")
				}
			}
		})
	}
}

type closeTrackingSource struct {
	source.Source
	closeCount atomic.Int32
}

func (s *closeTrackingSource) Close() error {
	s.closeCount.Add(1)
	return s.Source.Close()
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

func TestRunDrainsSources(t *testing.T) {
	sourceErr := errors.New("source failed")
	for _, tc := range []struct {
		name string
		err  error
		want int32
	}{
		{name: "wait for all inputs", want: 9},
		{name: "report source failure after draining", err: sourceErr, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			firstDone := make(chan struct{})
			first := &scriptedSource{run: func(ctx context.Context, emit func(*event.Event) error) error {
				defer close(firstDone)
				if err := emit(&event.Event{Kind: "data", Fields: map[string]any{"packets": 1}}); err != nil {
					return err
				}
				return tc.err
			}}
			second := &scriptedSource{run: func(ctx context.Context, emit func(*event.Event) error) error {
				<-firstDone
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(10 * time.Millisecond):
				}
				for range 8 {
					if err := emit(&event.Event{Kind: "data", Fields: map[string]any{"packets": 1}}); err != nil {
						return err
					}
				}
				return nil
			}}
			out := &countingSink{}
			app := &App{
				logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
				sources:          []source.Source{first, second},
				decoder:          noopDecoder{},
				processor:        passthroughProcessor{},
				processorWorkers: 1,
				encoderCfg:       config.EncoderConfig{Type: "json"},
				encoderWorkers:   1,
				sink:             out,
			}
			if err := app.Run(ctx); !errors.Is(err, tc.err) {
				t.Fatalf("Run error = %v, want %v", err, tc.err)
			}
			if ctx.Err() != nil {
				t.Fatalf("Run did not complete before its deadline")
			}
			if got := out.sendCount.Load(); got != tc.want {
				t.Fatalf("encoded %d records, want %d", got, tc.want)
			}
			if first.closeCount.Load() != 1 || second.closeCount.Load() != 1 {
				t.Fatal("Run must close every source")
			}
		})
	}
}

type scriptedSource struct {
	blockingSource
	run func(context.Context, func(*event.Event) error) error
}

func (s *scriptedSource) Start(ctx context.Context, emit func(*event.Event) error) error {
	return s.run(ctx, emit)
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
