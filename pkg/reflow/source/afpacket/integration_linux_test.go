//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package afpacket

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

// Run explicitly inside a container or network namespace with CAP_NET_RAW.
func TestCaptureLoopbackIntegration(t *testing.T) {
	if os.Getenv("REFLOW_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set REFLOW_CAPTURE_INTEGRATION=1 in an isolated Linux network namespace")
	}
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	promisc := false
	d := &driver{cfg: config.SourceConfig{Capture: config.CaptureConfig{Interface: "lo", SnapLen: 65535, Direction: "both", Promiscuous: &promisc}}, index: iface.Index}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	marker := []byte("reflow-afpacket-capture-fixture")
	go func() {
		done <- d.Run(ctx, func(f capture.Frame) error {
			if bytes.Contains(f.Data, marker) {
				return io.EOF
			}
			return nil
		})
	}()
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	writer, err := net.Dial("udp4", listener.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Close() }()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != io.EOF {
				t.Fatalf("capture failed: %v", err)
			}
			return
		case <-ctx.Done():
			t.Fatal("no loopback packet captured")
		case <-ticker.C:
			if _, err = writer.Write(marker); err != nil {
				t.Fatal(err)
			}
		}
	}
}
