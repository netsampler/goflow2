package pktmon

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func callbackFixture(t *testing.T, every int) (config.SourceConfig, *callbackQueue, []byte, chan struct{}) {
	t.Helper()
	cfg := config.SourceConfig{Network: "pktmon", Capture: config.CaptureConfig{Interface: "fixture", SnapLen: 256,
		Pktmon: config.PktmonConfig{QueueSize: 1}, Sampling: config.SamplingConfig{Location: "callback", Every: every,
			Adaptive: config.AdaptiveSamplingConfig{Enabled: true, MaxEvery: 16, IntervalMS: 10}}}}
	if err := config.NormalizeCapture(&cfg); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	q := newCallbackQueue(cfg.Capture, done)
	q.ports[7] = identity{Name: "fixture", Index: 77}
	metadata := make([]byte, 48)
	binary.LittleEndian.PutUint16(metadata[12:], 3)
	binary.LittleEndian.PutUint16(metadata[14:], 1)
	binary.LittleEndian.PutUint16(metadata[16:], 7)
	return cfg, q, metadata, done
}

func TestCallbackQueuePreservesSamplingAndBufferOwnership(t *testing.T) {
	cfg, q, metadata, done := callbackFixture(t, 4)
	defer close(done)
	data := []byte{1, 2, 3, 4}
	for range 4096 {
		q.receive(data, metadata)
		if len(q.queue) > 0 {
			break
		}
	}
	if len(q.queue) == 0 {
		t.Fatal("no random sample queued")
	}
	// With the consumer stalled, only randomly selected packets can become
	// queue losses. Every other observation is an intentional exclusion.
	for range 512 {
		q.receive(data, metadata)
	}
	if q.queueLoss.Load() == 0 || q.queueLoss.Load() != q.selected.Load()-1 || q.selected.Load() >= 512 {
		t.Fatalf("exclusions counted as losses: selected=%d queue_losses=%d", q.selected.Load(), q.queueLoss.Load())
	}
	q.recordMissed(2, 3)
	controller := capture.NewSamplingController(cfg.Capture.Sampling.Adaptive, 4)
	q.updateSampling(time.Unix(1700000000, 0), controller, callbackStats{})
	if q.rate.Load() != 16 {
		t.Fatalf("overload divisor = %d", q.rate.Load())
	}
	old := <-q.queue
	data[0] = 9 // Native streaming buffers may be immediately reused.
	if old.Data[0] != 1 || old.Sampling.Rate != 4 {
		t.Fatalf("queued frame was relabeled or borrowed: %+v", old)
	}
	for range 4096 {
		q.receive(data, metadata)
		if len(q.queue) > 0 {
			break
		}
	}
	if len(q.queue) == 0 {
		t.Fatal("no sample at updated divisor")
	}
	newFrame := <-q.queue
	if newFrame.Sampling.Rate != 16 || newFrame.Drops != q.queueLoss.Load()+5 {
		t.Fatalf("callback sampling/loss accounting: %+v", newFrame)
	}
	src, err := capture.New(cfg, &queuedFrames{frames: []capture.Frame{old, newFrame}})
	if err != nil {
		t.Fatal(err)
	}
	var packets []*event.Event
	if err := src.Start(context.Background(), func(evt *event.Event) error {
		if evt.Kind != "control" {
			packets = append(packets, evt)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(packets) != 2 {
		t.Fatalf("callback samples were sampled a second time: %d packets", len(packets))
	}
	for i, want := range []uint32{4, 16} {
		if packets[i].Source.Sampling.Rate != want || packets[i].Source.Sampling.SamplePool != 0 || packets[i].Source.Sampling.Drops != 0 || packets[i].Fields["effective_sampling_rate"] != uint64(want) {
			t.Fatalf("packet %d sampling accounting: %+v", i, packets[i])
		}
		if _, exists := packets[i].Fields["upstream_sampling_rate"]; exists {
			t.Fatal("local callback divisor presented as upstream sampling")
		}
	}
	refresh, err := src.InitEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(refresh) != 1 || refresh[0].Source.Sampling.Rate != 16 || refresh[0].Source.Sampling.CaptureDropsTotal != newFrame.Drops {
		t.Fatalf("source refresh lost callback rate or delivery loss: %+v", refresh)
	}
}

func TestCallbackMonitorAdaptsWhileConsumerStalls(t *testing.T) {
	_, q, metadata, done := callbackFixture(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	q.receive([]byte{1, 2, 3}, metadata)
	stop := q.startMonitor(ctx)
	defer stop()
	var writers sync.WaitGroup
	for range 4 {
		writers.Go(func() {
			for range 1000 {
				q.receive([]byte{1, 2, 3}, metadata)
			}
		})
	}
	writers.Wait()
	for q.rate.Load() == 1 && ctx.Err() == nil {
		time.Sleep(time.Millisecond)
	}
	if q.rate.Load() == 1 || q.losses() == 0 {
		t.Fatal("stalled queue did not trigger adaptation")
	}
	if queued := <-q.queue; queued.Sampling.Rate != 1 {
		t.Fatal("monitor relabeled a queued packet")
	}
	close(done)
	stopped := make(chan struct{})
	go func() { stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("monitor did not stop on closure")
	}
}

type queuedFrames struct{ frames []capture.Frame }

func (d *queuedFrames) Run(_ context.Context, emit func(capture.Frame) error) error {
	for _, frame := range d.frames {
		if err := emit(frame); err != nil {
			return err
		}
	}
	return nil
}
func (*queuedFrames) Close() error { return nil }
