//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package nflog

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

// Run only inside an isolated namespace whose firewall logs output to group 42.
func TestFirewallCopyIntegration(t *testing.T) {
	if os.Getenv("REFLOW_NFLOG_INTEGRATION") != "1" {
		t.Skip("set REFLOW_NFLOG_INTEGRATION=1 with an isolated NFLOG group 42 rule")
	}
	cfg := config.SourceConfig{Network: "nflog", Capture: config.CaptureConfig{NFLog: config.NFLogConfig{Group: 42}}}
	if err := config.NormalizeCapture(&cfg); err != nil {
		t.Fatal(err)
	}
	d := &driver{cfg: cfg, conn: netlinkcapture.NewConn()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	marker := []byte("reflow-nflog-packet-copy")
	done := make(chan error, 1)
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
			t.Fatal("no NFLOG packet copy captured")
		case <-ticker.C:
			if _, err = writer.Write(marker); err != nil {
				t.Fatal(err)
			}
		}
	}
}
