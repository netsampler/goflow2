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

func BenchmarkRunPipeline(b *testing.B) {
	benchmarks := []struct {
		name       string
		newEvent   func() *event.Event
		encoderCfg config.EncoderConfig
	}{
		{
			name:       "bytes_to_json",
			newEvent:   newBenchPacketEvent,
			encoderCfg: config.EncoderConfig{Type: "json"},
		},
		{
			name:       "bytes_to_protobuf",
			newEvent:   newBenchPacketEvent,
			encoderCfg: config.EncoderConfig{Type: "protobuf"},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			src := &benchSource{
				count:    b.N,
				newEvent: bm.newEvent,
			}
			out := &benchSink{}
			dec := decode.New()
			defer dec.Close()
			app := &App{
				logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
				sources:          []source.Source{src},
				decoder:          dec,
				processor:        processor.NewBuiltin(config.ProcessorConfig{}),
				processorWorkers: 1,
				encoderCfg:       bm.encoderCfg,
				encoderWorkers:   1,
				sink:             out,
			}

			b.ReportAllocs()
			b.ResetTimer()
			if err := app.Run(context.Background()); err != nil {
				b.Fatalf("Run returned error: %v", err)
			}
			b.StopTimer()
			if got := out.count.Load(); got != int64(b.N) {
				b.Fatalf("expected %d sink payloads, got %d", b.N, got)
			}
		})
	}
}

type benchSource struct {
	count    int
	newEvent func() *event.Event
}

func (s *benchSource) InitEvents() ([]*event.Event, error) {
	return nil, nil
}

func (s *benchSource) Start(ctx context.Context, emit func(*event.Event) error) error {
	for i := 0; i < s.count; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := emit(s.newEvent()); err != nil {
			return err
		}
	}
	return nil
}

func (s *benchSource) Close() error {
	return nil
}

type benchSink struct {
	count atomic.Int64
	bytes atomic.Int64
}

func (s *benchSink) Send(payload []byte) error {
	s.count.Add(1)
	s.bytes.Add(int64(len(payload)))
	return nil
}

func (s *benchSink) Close() error {
	return nil
}

func newBenchPacketEvent() *event.Event {
	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x14,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	return &event.Event{
		ReceivedAt: time.Unix(1_700_000_001, 0).UTC(),
		Source:     event.SourceMetadata{Type: "bytes"},
		Payload:    packet,
	}
}

var _ source.Source = (*benchSource)(nil)
var _ sink.Sink = (*benchSink)(nil)
