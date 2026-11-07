package capture

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

type fixtureDriver struct{ run func(func(Frame) error) error }

func (d *fixtureDriver) Run(_ context.Context, emit func(Frame) error) error { return d.run(emit) }
func (d *fixtureDriver) Close() error                                        { return nil }

func TestCaptureRefreshRetainsBackendDrops(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	var iface net.Interface
	for _, candidate := range ifaces {
		if candidate.Flags&net.FlagLoopback != 0 {
			iface = candidate
			break
		}
	}
	if iface.Index == 0 {
		t.Skip("requires a loopback interface")
	}
	driver := &fixtureDriver{run: func(emit func(Frame) error) error {
		for _, drops := range []uint64{0, 5, 1<<40 + 9} {
			if err := emit(Frame{Data: []byte{0x45, 0, 0, 20}, LinkType: 101,
				Interface: iface.Name, InterfaceIndex: iface.Index, Drops: drops}); err != nil {
				return err
			}
		}
		return nil
	}}
	src, err := New(config.SourceConfig{Network: "af_packet", Capture: config.CaptureConfig{
		Interface: iface.Name, SampleEvery: 1}}, driver)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.InitEvents(); err != nil {
		t.Fatal(err)
	}
	if err := src.Start(context.Background(), func(*event.Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		events, err := src.InitEvents()
		if err != nil {
			t.Fatal(err)
		}
		for _, evt := range events {
			init := evt.Payload.(event.SourceInit)
			if init.CaptureDropsTotal != 1<<40+9 || evt.Source.Sampling.CaptureDropsTotal != 1<<40+9 || init.Drops != 0 || init.SamplePool != 3 || init.SamplingRate != 1 {
				t.Fatalf("refresh lost capture counters: %+v, sampling %+v", init, evt.Source.Sampling)
			}
		}
	}
}

func TestCaptureOwnsSelectedBuffersAndExporterSampling(t *testing.T) {
	data := []byte{0x45, 0, 0, 20}
	stamp := time.Unix(100, 0).UTC()
	driver := &fixtureDriver{run: func(emit func(Frame) error) error {
		for i := 0; i < 3; i++ {
			data[1] = byte(i)
			if err := emit(Frame{Data: data, Timestamp: stamp, LinkType: 101, WireLength: 60, Sampling: &event.SamplingMetadata{Rate: 100, SamplePool: 900}, Drops: 2}); err != nil {
				return err
			}
		}
		return nil
	}}
	src, err := New(config.SourceConfig{Network: "psample", Capture: config.CaptureConfig{SampleEvery: 2}}, driver)
	if err != nil {
		t.Fatal(err)
	}
	var events []*event.Event
	if err := src.Start(context.Background(), func(e *event.Event) error { events = append(events, e); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("selected %d packets", len(events))
	}
	for i, e := range events {
		if e.Payload.([]byte)[1] != byte(i*2) {
			t.Fatal("borrowed buffer escaped capture callback")
		}
		if e.Source.Sampling.Rate != 200 || e.Source.Sampling.SamplePool != 900 || e.Fields["local_sample_every"] != 2 || e.Fields["capture_drops"] != uint64(2) {
			t.Fatalf("sampling/loss metadata: %#v %#v", e.Source.Sampling, e.Fields)
		}
		if e.ReceivedAt != stamp || e.Fields["wire_length"] != 60 || e.Fields["protocol"] != uint32(11) {
			t.Fatalf("capture metadata: %#v", e)
		}
	}
}

func TestNormalizeCookedAndLoopbackFrames(t *testing.T) {
	sll2 := make([]byte, 24)
	binary.BigEndian.PutUint16(sll2, 0x0800)
	binary.BigEndian.PutUint32(sll2[4:], 7)
	sll2[10] = 4
	sll2[20] = 0x45
	f := NormalizeFrame(Frame{Data: sll2, LinkType: 276, CaptureLength: 24, WireLength: 80})
	if len(f.Data) != 18 || f.Data[14] != 0x45 || f.Direction != "egress" || f.InterfaceIndex != 7 || f.WireLength != 80 {
		t.Fatalf("cooked frame: %#v", f)
	}
	loop := []byte{2, 0, 0, 0, 0x45, 0, 0, 20}
	f = NormalizeFrame(Frame{Data: loop, LinkType: 0})
	if len(f.Data) != 4 || f.Fields["protocol"] != uint32(11) {
		t.Fatalf("loopback: %#v", f)
	}
}

func TestCaptureKernelSamplesAreNotSampledTwice(t *testing.T) {
	driver := &fixtureDriver{run: func(emit func(Frame) error) error {
		for range 3 {
			if err := emit(Frame{Data: []byte{0x45, 0, 0, 20}, LinkType: 101,
				Sampling: &event.SamplingMetadata{Rate: 16}}); err != nil {
				return err
			}
		}
		return nil
	}}
	src, err := New(config.SourceConfig{Network: "af_packet", Capture: config.CaptureConfig{
		Interface: "fixture", Sampling: config.SamplingConfig{Location: "kernel", Every: 16}}}, driver)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := src.Start(context.Background(), func(evt *event.Event) error {
		count++
		if evt.Source.Sampling.Rate != 16 || evt.Source.Sampling.SamplePool != 0 || evt.Fields["effective_sampling_rate"] != uint64(16) {
			t.Fatalf("kernel sampler metadata: %+v, %+v", evt.Source.Sampling, evt.Fields)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("backend selected 3 samples; adapter emitted %d", count)
	}
}
