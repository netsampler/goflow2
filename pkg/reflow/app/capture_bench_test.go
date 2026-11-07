package app

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/decode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/processor"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

// A fixed set of IPv6/TCP flows with truncated headers, original wire lengths,
// real capture adaptation, processing, aggregation and IPFIX encode/decode.
// The fixture cannot drop packets, making processing capacity measurable
// independently of the NIC/ring while checking that all counters survive.
func BenchmarkCaptureAggregateIPFIX(b *testing.B) {
	ifaces, err := net.Interfaces()
	if err != nil {
		b.Fatal(err)
	}
	var iface net.Interface
	for _, candidate := range ifaces {
		if candidate.Flags&net.FlagLoopback != 0 {
			iface = candidate
			break
		}
	}
	if iface.Index == 0 {
		b.Skip("requires loopback")
	}
	for _, flows := range []int{32, 1000} {
		for _, workers := range []int{1, 2} {
			b.Run(fmt.Sprintf("flows=%d/workers=%d", flows, workers), func(b *testing.B) {
				reset := true
				idle, period := 10000, 60000
				cfg, _, err := config.LoadFromFlags(&config.FlagConfig{
					Inputs: []string{"af_packet:" + iface.Name + ":bytes?snaplen=256&sample_every=1&direction=both"},
					Output: "ipfix:udp:[::1]:4739?resolve_interval_ms=0", Aggregate: true,
					AggIdleFlushAfter: &idle, AggPeriodicEvery: &period, AggResetBuckets: &reset,
				})
				if err != nil {
					b.Fatal(err)
				}
				driver := &captureBenchDriver{count: b.N, flows: flows, iface: iface}
				src, err := capture.New(cfg.Sources[0], driver)
				if err != nil {
					b.Fatal(err)
				}
				out := &decodedCounterSink{decoder: decode.NewWithCatalog(cfg.TemplatedFields.Catalog)}
				app := &App{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), sources: []source.Source{src},
					decoder: decode.New(), processor: processor.NewBuiltin(cfg.Processor), processorWorkers: workers,
					aggregatorCfgs: cfg.Aggregators, encoderCfg: cfg.Encoder, encoderWorkers: 1, sink: out}
				b.ReportAllocs()
				b.SetBytes(1514)
				b.ResetTimer()
				if err := app.Run(context.Background()); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				if out.err != nil {
					b.Fatal(out.err)
				}
				if out.packets != int64(b.N) || out.bytes != int64(b.N)*1514 {
					b.Fatalf("counter mismatch packets=%d bytes=%d N=%d", out.packets, out.bytes, b.N)
				}
			})
		}
	}
}

type captureBenchDriver struct {
	count, flows int
	iface        net.Interface
}

func (d *captureBenchDriver) Close() error { return nil }
func (d *captureBenchDriver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	header := make([]byte, 256)
	binary.BigEndian.PutUint16(header[12:14], 0x86dd)
	header[14] = 0x60
	binary.BigEndian.PutUint16(header[18:20], 1514-14-40)
	header[20], header[21] = 6, 64
	header[22], header[23], header[24], header[25], header[37] = 0x20, 1, 0x0d, 0xb8, 1
	header[38], header[39], header[40], header[41], header[53] = 0x20, 1, 0x0d, 0xb8, 2
	binary.BigEndian.PutUint16(header[54:56], 443)
	header[66], header[67] = 0x50, 0x18
	stamp := time.Now().UTC()
	for i := 0; i < d.count; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		binary.BigEndian.PutUint16(header[56:58], uint16(50000+i%d.flows))
		if err := emit(capture.Frame{Data: header, Timestamp: stamp, LinkType: 1,
			Interface: d.iface.Name, InterfaceIndex: d.iface.Index, Direction: "ingress",
			PacketType: "host", CaptureLength: 256, WireLength: 1514}); err != nil {
			return err
		}
	}
	return nil
}
