//go:build reflow_pcap && cgo && !reflow_dpdk

package pcap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

// Opt in on a host with capture permission to exercise the native C ABI and
// actual packet delivery. All generated traffic stays on the local loopback.
func TestLiveLoopback(t *testing.T) {
	if os.Getenv("REFLOW_LIVE_CAPTURE") != "1" {
		t.Skip("set REFLOW_LIVE_CAPTURE=1 on a host with loopback capture permission")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows loopback capture requires an explicit Npcap device identifier")
	}
	device := "lo0"
	if runtime.GOOS == "linux" {
		device = "lo"
	}
	receiver, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = receiver.Close() }()
	sender, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sender.Close() }()
	promisc := false
	src, err := New(config.SourceConfig{Network: "pcap_live", Type: "bytes", Capture: config.CaptureConfig{Interface: device, Promiscuous: &promisc}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	marker := []byte("reflow-portable-libpcap-loopback-fixture")
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = sender.WriteTo(marker, receiver.LocalAddr())
			}
		}
	}()
	var captured []byte
	err = src.Start(ctx, func(evt *event.Event) error {
		if evt.Kind == "control" {
			return nil
		}
		data := evt.Payload.([]byte)
		if !bytes.Contains(data, marker) {
			return nil
		}
		captured = data
		if evt.ReceivedAt.IsZero() || evt.Fields["capture_length"] == nil || evt.Fields["wire_length"] == nil {
			t.Errorf("missing native capture metadata: %#v", evt)
		}
		return io.EOF
	})
	cancel()
	<-done
	if !errors.Is(err, io.EOF) {
		t.Fatalf("loopback packet was not captured: %v", err)
	}
	if !bytes.Contains(captured, marker) {
		t.Fatal("captured buffer was not retained after handle closure")
	}
}
