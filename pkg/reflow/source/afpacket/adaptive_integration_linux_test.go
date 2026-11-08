//go:build linux && !reflow_pcap && !reflow_dpdk

package afpacket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

// A deliberately stalled consumer fills a small ring. The stats monitor must
// notice the delivery loss while emit blocks, and queued old-epoch packets must
// retain their original rate when the reader resumes.
func TestAdaptiveCaptureEpochIntegration(t *testing.T) {
	if os.Getenv("REFLOW_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set REFLOW_CAPTURE_INTEGRATION=1 in an isolated Linux network namespace")
	}
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	promisc := false
	cfg := config.SourceConfig{Network: "af_packet", Capture: config.CaptureConfig{
		Interface: "lo", SnapLen: 256, Direction: "both", Promiscuous: &promisc,
		AFPacket: config.AFPacketConfig{BlockSize: 4096, NumBlocks: 2, BlockTimeoutMS: 10},
		Sampling: config.SamplingConfig{Location: "kernel", Every: 1, Adaptive: config.AdaptiveSamplingConfig{
			Enabled: true, MinEvery: 1, MaxEvery: 8, IntervalMS: 10, RecoveryAfterMS: 30000, IncreaseFactor: 2,
		}},
	}}
	if err := config.NormalizeCapture(&cfg); err != nil {
		t.Fatal(err)
	}
	d := &driver{cfg: cfg, index: iface.Index}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	go func() { _, _ = io.Copy(io.Discard, listenerReader{listener}) }()
	writer, err := net.Dial("udp4", listener.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	marker := []byte("reflow-adaptive-epoch-fixture")
	replacementMarker := []byte("reflow-replacement-live-during-drain")
	stalled := make(chan struct{}, 1)
	releaseInitial := make(chan struct{})
	draining := make(chan struct{}, 1)
	releaseDrain := make(chan struct{})
	var drainReleased time.Time
	drainBlocked := false
	done := make(chan error, 1)
	runDone := make(chan struct{})
	oldFrames, newFrames := 0, 0
	var finalDrops uint64
	var pause uint64
	go func() {
		defer close(runDone)
		done <- d.Run(ctx, func(f capture.Frame) error {
			if !bytes.Contains(f.Data, marker) && !bytes.Contains(f.Data, replacementMarker) {
				return nil
			}
			if f.Sampling == nil {
				return fmt.Errorf("missing packet sampling metadata")
			}
			if f.Drops > finalDrops {
				finalDrops = f.Drops
			}
			if f.Sampling.Rate == 1 {
				if newFrames > 0 {
					return fmt.Errorf("old epoch appeared after new epoch")
				}
				oldFrames++
				if transition, _ := f.Fields["capture_sampling_transitions"].(uint64); transition > 0 && !drainBlocked {
					drainBlocked = true
					draining <- struct{}{}
					select {
					case <-releaseDrain:
						drainReleased = time.Now()
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				if oldFrames == 1 {
					stalled <- struct{}{}
					select {
					case <-releaseInitial:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			} else {
				newFrames++
				pause, _ = f.Fields["capture_sampling_pause_total_ns"].(uint64)
				if oldFrames < 2 {
					return fmt.Errorf("old queued epoch did not drain")
				}
				if pause == 0 {
					return fmt.Errorf("sampling transition did not report capture pause")
				}
				if bytes.Contains(f.Data, replacementMarker) {
					if drainReleased.IsZero() || !f.Timestamp.Before(drainReleased) {
						return fmt.Errorf("replacement did not capture while old ring was draining")
					}
					if complete, _ := f.Fields["capture_sampling_pause_counters_complete"].(bool); !complete {
						return fmt.Errorf("transition did not retain interface byte measurements")
					}
					return io.EOF
				}
			}
			return nil
		})
	}()
	defer func() {
		cancel()
		<-runDone
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != io.EOF {
				t.Fatalf("adaptive capture: %v", err)
			}
			if finalDrops == 0 || newFrames == 0 {
				t.Fatalf("missing overload transition: old=%d new=%d drops=%d", oldFrames, newFrames, finalDrops)
			}
			t.Logf("old epoch records=%d new epoch records=%d drops=%d capture pause=%s", oldFrames, newFrames, finalDrops, time.Duration(pause))
			return
		case <-stalled:
			// Fill the old ring while its consumer blocks, then stop the load
			// before allowing rotation. Otherwise ordinary load can fill the new
			// ring before the replacement-marker traffic starts, dropping every
			// marker and timing out despite a successful sampling transition.
			ticker.Stop()
			until := time.Now().Add(150 * time.Millisecond)
			for time.Now().Before(until) {
				if _, err := writer.Write(marker); err != nil {
					t.Fatal(err)
				}
			}
			close(releaseInitial)
		case <-draining:
			// This traffic stops before the old consumer is released. Receiving
			// it later with the new divisor proves the replacement was already
			// capturing during old-ring backpressure, rather than after drain.
			for range 100 {
				if _, err := writer.Write(replacementMarker); err != nil {
					t.Fatal(err)
				}
				time.Sleep(time.Millisecond)
			}
			ticker.Stop()
			close(releaseDrain)
		case <-ctx.Done():
			<-runDone
			t.Fatalf("adaptive capture did not finish: old=%d new=%d drain_blocked=%t", oldFrames, newFrames, drainBlocked)
		case <-ticker.C:
			for range 300 {
				if _, err := writer.Write(marker); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

type listenerReader struct{ net.PacketConn }

func (r listenerReader) Read(p []byte) (int, error) { n, _, err := r.ReadFrom(p); return n, err }
