//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package ebpf

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	cilium "github.com/cilium/ebpf"
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
	testKernelSamplingLoopback(t, false, "perf", "kernel")
}

func TestAdaptiveKernelSamplingLoopbackIntegration(t *testing.T) {
	testKernelSamplingLoopback(t, true, "perf", "kernel")
}

func TestRingKernelSamplingLoopbackIntegration(t *testing.T) {
	testKernelSamplingLoopback(t, false, "ringbuf", "kernel")
}
func TestRingAdaptiveKernelSamplingLoopbackIntegration(t *testing.T) {
	testKernelSamplingLoopback(t, true, "ringbuf", "kernel")
}
func TestAutoKernelSamplingLoopbackIntegration(t *testing.T) {
	testKernelSamplingLoopback(t, false, "auto", "kernel")
}

func TestRingUserspaceSamplingLoopbackIntegration(t *testing.T) {
	testKernelSamplingLoopback(t, false, "ringbuf", "userspace")
}

func testKernelSamplingLoopback(t *testing.T, adaptive bool, transport, location string) {
	t.Helper()
	if os.Getenv("REFLOW_EBPF_INTEGRATION") != "1" {
		t.Skip("set REFLOW_EBPF_INTEGRATION=1 with BPF and perf capabilities")
	}
	no := false
	sampling := config.SamplingConfig{Location: location, Every: 8}
	if adaptive {
		sampling.Every = 1
		sampling.Adaptive = config.AdaptiveSamplingConfig{Enabled: true, MaxEvery: 8, IntervalMS: 10}
	}
	src, err := New(config.SourceConfig{Network: "ebpf", Capture: config.CaptureConfig{Interface: "lo", SnapLen: 256,
		Sampling: sampling, EBPF: config.EBPFConfig{Conntrack: &no, Transport: transport, RingSize: 4096}}})
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
	runDone := make(chan struct{})
	oldRecords := 0
	transitioned := false
	go func() {
		defer close(runDone)
		done <- src.Start(ctx, func(evt *event.Event) error {
			packet, ok := evt.Payload.([]byte)
			if !ok || !bytes.Contains(packet, marker) {
				return nil
			}
			if evt.Source.Sampling == nil || (location == "kernel" && evt.Source.Sampling.SamplePool != 0) || (location == "userspace" && evt.Source.Sampling.SamplePool == 0) {
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
				} else if evt.Source.Sampling.Rate < 2 || evt.Source.Sampling.CaptureDropsTotal == 0 {
					return fmt.Errorf("missing backlog-driven transition: old=%d metadata=%+v", oldRecords, evt.Source.Sampling)
				} else {
					transitioned = true
				}
				// Perf buffers are per CPU: an updated divisor on one CPU may
				// arrive before old queued samples on another. Require both
				// observations without assuming a global ordering between them.
				if !transitioned || oldRecords < 2 {
					return nil
				}
			} else if evt.Source.Sampling.Rate != 8 {
				return fmt.Errorf("unexpected divisor: %+v", evt.Source.Sampling)
			}
			return io.EOF
		})
	}()
	defer func() {
		cancel()
		<-runDone
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
			<-runDone
			t.Fatalf("sampled SKB capture did not finish: old=%d transitioned=%t", oldRecords, transitioned)
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

// Source options must report current loss and divisor even if a congested
// output has not delivered a frame to the shared adapter yet.
func TestRingHealthRefreshWithoutDeliveredFramesIntegration(t *testing.T) {
	if os.Getenv("REFLOW_EBPF_INTEGRATION") != "1" {
		t.Skip("set REFLOW_EBPF_INTEGRATION=1 with BPF access")
	}
	no := false
	src, err := New(config.SourceConfig{Network: "ebpf", Capture: config.CaptureConfig{Interface: "lo", SnapLen: 128, Sampling: config.SamplingConfig{Location: "kernel", Every: 1}, EBPF: config.EBPFConfig{Transport: "ringbuf", RingSize: 4096, Conntrack: &no}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	backend, err := src.prepareRingCapture()
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	// Do not consume the ring: enough helper calls must fill it and fail.
	if _, err := backend.program.Run(&cilium.RunOptions{Data: make([]byte, 64), Repeat: 512}); err != nil {
		t.Fatal(err)
	}
	stats, err := backend.sampler.ReadStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Failed == 0 {
		t.Fatal("fixture did not congest the ring")
	}
	if err := backend.sampler.SetRate(64); err != nil {
		t.Fatal(err)
	}
	src.kernelSampler = backend.sampler
	controls, err := src.InitEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(controls) != 1 {
		t.Fatalf("controls=%d", len(controls))
	}
	control := controls[0]
	payload := control.Payload.(event.SourceInit)
	if control.Source.Sampling.Rate != 64 || control.Source.Sampling.CaptureDropsTotal != stats.Failed || payload.SamplingRate != 64 || payload.CaptureDropsTotal != stats.Failed {
		t.Fatalf("stale health: source=%+v payload=%+v", control.Source.Sampling, payload)
	}
}

func TestRingSnapshotPreservesWireLengthIntegration(t *testing.T) {
	if os.Getenv("REFLOW_EBPF_INTEGRATION") != "1" {
		t.Skip("set REFLOW_EBPF_INTEGRATION=1 with BPF access")
	}
	no := false
	src, err := New(config.SourceConfig{Network: "ebpf", Capture: config.CaptureConfig{Interface: "lo", SnapLen: 256, Sampling: config.SamplingConfig{Location: "kernel", Every: 1}, EBPF: config.EBPFConfig{Transport: "ringbuf", Conntrack: &no}}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	backend, err := src.prepareRingCapture()
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	src.backendSnaplen = backend.snaplen
	packet := make([]byte, 1500)
	for i := range packet {
		packet[i] = byte(i % 251)
	}
	if _, _, err := backend.program.Test(packet); err != nil {
		t.Fatal(err)
	}
	backend.reader.SetDeadline(time.Now().Add(time.Second))
	record, err := backend.reader.Read()
	if err != nil {
		t.Fatal(err)
	}
	if err := src.emitRingSample(record.RawSample, func(evt *event.Event) error {
		got := evt.Payload.([]byte)
		// BPF_PROG_TEST_RUN removes the synthetic Ethernet header before
		// invoking a socket filter, unlike the attached AF_PACKET hook.
		if len(got) != 256 || !bytes.Equal(got, packet[14:14+256]) || evt.Fields["wire_length"] != len(packet)-14 || evt.Fields["capture_backend_snaplen"] != 256 || evt.Source.Sampling.Rate != 1 {
			t.Fatalf("snapshot changed wire length or framing: len=%d fields=%+v sampling=%+v", len(got), evt.Fields, evt.Source.Sampling)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
