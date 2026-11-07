//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

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

// Compare two independent capture sockets while checking that sampled-out
// packets still reach the UDP receiver. Run with CAP_NET_RAW and no other load.
func TestKernelSamplingLoopbackIntegration(t *testing.T) {
	if os.Getenv("REFLOW_CAPTURE_INTEGRATION") != "1" {
		t.Skip("set REFLOW_CAPTURE_INTEGRATION=1 in an isolated Linux network namespace")
	}
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		t.Fatal(err)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	marker := []byte("reflow-kernel-sampling-integration")
	promisc := false
	type result struct {
		packets int
		err     error
	}
	start := func(every int) <-chan result {
		done := make(chan result, 1)
		d := &driver{cfg: config.SourceConfig{Capture: config.CaptureConfig{
			Interface: "lo", SnapLen: 256, Direction: "both", Promiscuous: &promisc,
			SampleEvery: every, Sampling: config.SamplingConfig{Location: "kernel", Algorithm: "random"},
		}}, index: iface.Index}
		go func() {
			packets := 0
			err := d.Run(ctx, func(f capture.Frame) error {
				if bytes.Contains(f.Data, marker) {
					packets++
					if f.Sampling == nil || f.Sampling.Rate != uint32(every) || f.Drops != 0 {
						return fmt.Errorf("incorrect sampling metadata: %+v, drops=%d", f.Sampling, f.Drops)
					}
				}
				return nil
			})
			done <- result{packets: packets, err: err}
		}()
		return done
	}
	baseline, sampled := start(1), start(8)
	const sent = 1000
	received := make(chan error, 1)
	go func() {
		if err := listener.SetReadDeadline(time.Now().Add(6 * time.Second)); err != nil {
			received <- err
			return
		}
		buf := make([]byte, 256)
		for range sent {
			n, _, err := listener.ReadFrom(buf)
			if err != nil {
				received <- err
				return
			}
			if !bytes.Equal(buf[:n], marker) {
				received <- fmt.Errorf("unexpected UDP payload")
				return
			}
		}
		received <- nil
	}()
	// Allow both capture sockets to install their filters and bind.
	time.Sleep(200 * time.Millisecond)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for range sent {
		<-ticker.C
		if _, err := writer.Write(marker); err != nil {
			t.Fatal(err)
		}
	}
	if err := <-received; err != nil {
		t.Fatalf("sampling affected UDP delivery: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // retire partially filled ring blocks
	cancel()
	all, selected := <-baseline, <-sampled
	if all.err != nil || selected.err != nil {
		t.Fatalf("capture failed: baseline=%v sampled=%v", all.err, selected.err)
	}
	if all.packets < sent || selected.packets*8 < all.packets*6/10 || selected.packets*8 > all.packets*14/10 {
		t.Fatalf("unexpected kernel sample ratio: selected=%d baseline=%d divisor=8", selected.packets, all.packets)
	}
	t.Logf("UDP delivered=%d, baseline=%d, sampled=%d, divisor=8", sent, all.packets, selected.packets)
}
