//go:build darwin && !reflow_pcap && !reflow_dpdk

package bpf

import (
	"bytes"
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

// This opt-in test opens real capture devices and requires root or BPF access.
func TestLiveLoopback(t *testing.T) {
	if os.Getenv("REFLOW_LIVE_CAPTURE") != "1" {
		t.Skip("set REFLOW_LIVE_CAPTURE=1 to test privileged live capture")
	}
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	sender, err := net.Dial("udp4", listener.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sender.Close() }()
	marker := []byte("reflow-bpf-live-" + listener.LocalAddr().String())
	src, err := New(config.SourceConfig{Network: "bpf", Type: "bytes", Capture: config.CaptureConfig{Interface: "lo0", SnapLen: 512}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	found := make(chan struct{}, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- src.Start(ctx, func(e *event.Event) error {
			if data, ok := e.Payload.([]byte); ok && bytes.Contains(data, marker) {
				select {
				case found <- struct{}{}:
				default:
				}
			}
			return nil
		})
	}()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err = sender.Write(marker); err != nil {
				t.Fatal(err)
			}
		case err = <-finished:
			t.Fatalf("capture stopped before receiving the fixture: %v", err)
		case <-ctx.Done():
			_ = src.Close()
			t.Fatal("capture did not receive the loopback UDP fixture")
		case <-found:
			cancel()
			if err = src.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("capture did not stop after cancellation")
			}
			return
		}
	}
}
