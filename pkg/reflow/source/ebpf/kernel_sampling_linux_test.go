//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package ebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func TestKernelSampleRetainsQueuedDivisorAndSKB(t *testing.T) {
	no := false
	s := &Source{cfg: config.SourceConfig{Network: "ebpf", Interface: "lo", Type: "bytes", SampleEvery: 1, SnapLen: 4,
		Capture: config.CaptureConfig{Sampling: config.SamplingConfig{Location: "kernel"}},
		EBPF:    config.EBPFConfig{SKBMetadata: &no}}, captureInterfaceIndex: 1}
	for _, rate := range []uint32{16, 64, 16} {
		sample := make([]byte, 4+testSKBMetadataSize()+4+4) // packet followed by perf alignment bytes
		binary.NativeEndian.PutUint32(sample, rate)
		binary.LittleEndian.PutUint32(sample[4:], 1514)
		copy(sample[4+testSKBMetadataSize():], []byte{1, 2, 3, 4})
		if err := s.emitPerfSample(sample, func(evt *event.Event) error {
			if evt.Source.Sampling.Rate != rate || evt.Source.Sampling.SamplePool != 0 || evt.Fields["wire_length"] != 1514 || !bytes.Equal(evt.Payload.([]byte), []byte{1, 2, 3, 4}) {
				t.Fatalf("sample lost its record divisor or framing: %+v", evt)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKernelSamplingLoopbackIntegration(t *testing.T) {
	testKernelSamplingLoopback(t, false)
}

func TestAdaptiveKernelSamplingLoopbackIntegration(t *testing.T) {
	testKernelSamplingLoopback(t, true)
}

func testKernelSamplingLoopback(t *testing.T, adaptive bool) {
	t.Helper()
	if os.Getenv("REFLOW_EBPF_INTEGRATION") != "1" {
		t.Skip("set REFLOW_EBPF_INTEGRATION=1 with BPF and perf capabilities")
	}
	no := false
	sampling := config.SamplingConfig{Location: "kernel", Every: 8}
	if adaptive {
		sampling.Every = 1
		sampling.Adaptive = config.AdaptiveSamplingConfig{Enabled: true, MaxEvery: 8, IntervalMS: 10}
	}
	src, err := New(config.SourceConfig{Network: "ebpf", Capture: config.CaptureConfig{Interface: "lo", SnapLen: 256,
		Sampling: sampling, EBPF: config.EBPFConfig{Conntrack: &no}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
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
	marker := []byte("reflow-sampled-skb-fixture")
	done := make(chan error, 1)
	oldRecords := 0
	go func() {
		done <- src.Start(ctx, func(evt *event.Event) error {
			packet, ok := evt.Payload.([]byte)
			if !ok || !bytes.Contains(packet, marker) {
				return nil
			}
			if evt.Source.Sampling == nil || evt.Source.Sampling.SamplePool != 0 {
				return fmt.Errorf("missing kernel sample metadata: %+v", evt.Source.Sampling)
			}
			if evt.Fields["capture_length"] != len(marker)+42 || evt.Fields["wire_length"] != len(marker)+42 {
				return fmt.Errorf("perf padding changed packet framing: %+v", evt.Fields)
			}
			if adaptive {
				if evt.Source.Sampling.Rate == 1 {
					oldRecords++
					if oldRecords == 1 {
						// Keep the perf consumer blocked while the independent
						// kernel counters drive a sampling map update.
						time.Sleep(150 * time.Millisecond)
					}
					return nil
				}
				if oldRecords < 2 || evt.Source.Sampling.Rate < 2 || evt.Source.Sampling.CaptureDropsTotal == 0 {
					return fmt.Errorf("missing backlog-driven transition: old=%d metadata=%+v", oldRecords, evt.Source.Sampling)
				}
			} else if evt.Source.Sampling.Rate != 8 {
				return fmt.Errorf("unexpected divisor: %+v", evt.Source.Sampling)
			}
			return io.EOF
		})
	}()
	forwarded := make(chan bool, 1)
	go func() {
		buf := make([]byte, 128)
		_ = listener.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, err := listener.ReadFrom(buf)
		forwarded <- err == nil && bytes.Equal(buf[:n], marker)
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			if err != io.EOF {
				t.Fatalf("sampled eBPF capture: %v", err)
			}
			if !<-forwarded {
				t.Fatal("sampling prevented ordinary UDP delivery")
			}
			return
		case <-ctx.Done():
			t.Fatal("no sampled SKB packet received")
		case <-ticker.C:
			count := 1
			if adaptive {
				count = 300
			}
			for range count {
				if _, err := writer.Write(marker); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}
