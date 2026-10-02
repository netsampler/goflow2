package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/decode"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/processor"
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

func TestRunCompletesAllSources(t *testing.T) {
	for _, live := range []bool{false, true} {
		name := "finite_inputs"
		if live {
			name = "finite_and_live_inputs"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			const records = 1001
			output := make(completionSink, records)
			firstFinished := make(chan struct{})
			emitRecord := func(emit func(*event.Event) error, id int) error {
				return emit(&event.Event{
					Source: event.SourceMetadata{
						Type: "json",
						JSON: event.JSONMetadata{Flavor: "reflow"},
					},
					Message: json.RawMessage(fmt.Sprintf(`{"id":%d}`, id)),
				})
			}
			app := &App{
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
				sources: []source.Source{
					completionSource(func(ctx context.Context, emit func(*event.Event) error) error {
						defer close(firstFinished)
						return emitRecord(emit, 0)
					}),
					completionSource(func(ctx context.Context, emit func(*event.Event) error) error {
						select {
						case <-firstFinished:
						case <-ctx.Done():
							return ctx.Err()
						}
						for id := 1; id < records; id++ {
							if err := emitRecord(emit, id); err != nil {
								return err
							}
						}
						if live {
							<-ctx.Done()
						}
						return nil
					}),
				},
				decoder:          decode.New(),
				processor:        processor.NewBuiltin(config.ProcessorConfig{}),
				processorWorkers: 1,
				encoderCfg:       config.EncoderConfig{Type: "json"},
				encoderWorkers:   1,
				sink:             output,
			}
			done := make(chan error, 1)
			go func() { done <- app.Run(ctx) }()
			seen := make(map[int]bool, records)
		collect:
			for {
				select {
				case payload, ok := <-output:
					if !ok {
						break collect
					}
					var decoded struct {
						Fields struct{ ID int } `json:"fields"`
					}
					if err := json.Unmarshal(payload, &decoded); err != nil {
						t.Fatal(err)
					}
					id := decoded.Fields.ID
					if id < 0 || id >= records || seen[id] {
						t.Fatalf("unexpected or duplicate record ID %d", id)
					}
					seen[id] = true
					if live && len(seen) == records {
						cancel()
					}
				case <-deadline.C:
					t.Fatal("timed out waiting for source output and shutdown")
				}
			}
			if err := <-done; err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if len(seen) != records {
				t.Fatalf("got %d records, want %d", len(seen), records)
			}
		})
	}
}

type completionSource func(context.Context, func(*event.Event) error) error

func (completionSource) InitEvents() ([]*event.Event, error) { return nil, nil }
func (s completionSource) Start(ctx context.Context, emit func(*event.Event) error) error {
	return s(ctx, emit)
}
func (completionSource) Close() error { return nil }

type completionSink chan []byte

func (s completionSink) Send(payload []byte) error {
	s <- append([]byte(nil), payload...)
	return nil
}
func (s completionSink) Close() error {
	close(s)
	return nil
}
