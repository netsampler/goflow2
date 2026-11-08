package app

import (
	"context"
	"encoding/binary"
	"flag"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/decode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/processor"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source"
)

func TestRunPreservesTruncatedWireCountersThroughIPFIX(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	flags, _ := config.BindFlags(fs)
	if err := fs.Parse(config.NormalizeAggregateArgs([]string{
		"-input", "af_packet:eth2:bytes?snaplen=256&sample_every=1&direction=both",
		"-agg", "idle_flush_after_ms=10000,periodic_every_ms=60000,reset_buckets=true",
		"-output", "ipfix:udp:[::1]:4739?resolve_interval_ms=0", "-genconf",
	})); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.LoadFromFlags(flags)
	if err != nil {
		t.Fatal(err)
	}
	const packets, wireLength = 1000, 65000
	header := make([]byte, 256)
	binary.BigEndian.PutUint16(header[12:14], 0x86dd)
	header[14] = 0x60
	binary.BigEndian.PutUint16(header[18:20], wireLength-14-40)
	header[20], header[21] = 6, 64
	header[22], header[23], header[24], header[25], header[37] = 0x20, 1, 0x0d, 0xb8, 1
	header[38], header[39], header[40], header[41], header[53] = 0x20, 1, 0x0d, 0xb8, 2
	binary.BigEndian.PutUint16(header[54:56], 443)
	binary.BigEndian.PutUint16(header[56:58], 59037)
	header[66], header[67] = 0x50, 0x18
	stamp := time.Now().UTC()
	src := &scriptedSource{run: func(_ context.Context, emit func(*event.Event) error) error {
		for range packets {
			if err := emit(&event.Event{
				ReceivedAt: stamp,
				Source: event.SourceMetadata{Type: "bytes", Network: "af_packet",
					CaptureInterface: "eth2", CaptureInterfaceIndex: 4, SourceIDSet: true,
					Sampling: &event.SamplingMetadata{Rate: 1}},
				Payload: header,
				Fields: map[string]any{"wire_length": int(wireLength), "input_if": uint32(4),
					"output_if": uint32(0), "flow_direction": uint32(0), "source_id": uint32(0)},
			}); err != nil {
				return err
			}
		}
		return nil
	}}
	out := &decodedCounterSink{decoder: decode.NewWithCatalog(cfg.TemplatedFields.Catalog)}
	app := &App{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), sources: []source.Source{src},
		decoder: decode.New(), processor: processor.NewBuiltin(cfg.Processor), processorWorkers: 2,
		aggregatorCfgs: cfg.Aggregators, encoderCfg: cfg.Encoder, encoderWorkers: 1, sink: out}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if out.err != nil {
		t.Fatal(out.err)
	}
	if out.packets != packets || out.bytes != packets*wireLength {
		t.Fatalf("IPFIX counted %d packets / %d bytes, want %d / %d", out.packets, out.bytes, packets, packets*wireLength)
	}
}

type decodedCounterSink struct {
	decoder decode.Decoder
	packets int64
	bytes   int64
	err     error
}

func (s *decodedCounterSink) Send(payload []byte) error {
	events, err := s.decoder.Decode(&event.Event{Payload: payload,
		Source: event.SourceMetadata{Network: "udp", Type: "flow", Remote: "[::1]:54408"}})
	if err != nil {
		s.err = err
		return err
	}
	for _, evt := range events {
		if evt.Fields["record_kind"] == "packet" {
			s.bytes += evt.Fields["bytes"].(int64)
			s.packets += evt.Fields["packets"].(int64)
		}
	}
	return nil
}

func (s *decodedCounterSink) Close() error {
	s.decoder.Close()
	return nil
}
