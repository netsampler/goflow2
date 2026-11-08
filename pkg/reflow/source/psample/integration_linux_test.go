//go:build linux && !reflow_pcap && !reflow_dpdk

package psample

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
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/netlinkcapture"
)

// Run in an isolated namespace with a tc sample action exporting group 7.
func TestSoftwareSampleIntegration(t *testing.T) {
	if os.Getenv("REFLOW_PSAMPLE_INTEGRATION") != "1" {
		t.Skip("set REFLOW_PSAMPLE_INTEGRATION=1 with a tc sample action for group 7")
	}
	cfg := config.SourceConfig{Network: "psample", Capture: config.CaptureConfig{PSample: config.PSampleConfig{Groups: []uint32{7}}}}
	if err := config.NormalizeCapture(&cfg); err != nil {
		t.Fatal(err)
	}
	d := &driver{cfg: cfg, conn: netlinkcapture.NewConn(), decoder: newDecoder([]uint32{7}, nil)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	marker := []byte("reflow-psample-software-export")
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx, func(f capture.Frame) error {
			if bytes.Contains(f.Data, marker) {
				if f.Sampling == nil || f.Sampling.Rate != 1 {
					return io.ErrUnexpectedEOF
				}
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
				t.Fatalf("psample capture failed: %v", err)
			}
			return
		case <-ctx.Done():
			t.Fatal("no software psample export captured")
		case <-ticker.C:
			if _, err = writer.Write(marker); err != nil {
				t.Fatal(err)
			}
		}
	}
}
