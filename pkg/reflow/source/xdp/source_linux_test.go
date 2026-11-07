//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package xdp

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func TestPerfPacketRecord(t *testing.T) {
	data := make([]byte, 32) // Includes perf's possible trailing alignment bytes.
	binary.NativeEndian.PutUint32(data, 1514)
	binary.NativeEndian.PutUint32(data[4:], 7)
	binary.NativeEndian.PutUint64(data[8:], 1234)
	binary.NativeEndian.PutUint32(data[16:], 4)
	copy(data[24:], []byte{1, 2, 3, 4})
	f, err := decodeRecord(data, 1700000000000000000)
	if err != nil {
		t.Fatal(err)
	}
	if f.WireLength != 1514 || f.CaptureLength != 4 || f.InterfaceIndex != 7 || !bytes.Equal(f.Data, []byte{1, 2, 3, 4}) || !f.Timestamp.Equal(time.Unix(0, 1700000000000001234)) {
		t.Fatalf("perf frame %+v", f)
	}
}

func TestPassiveProgramIntegration(t *testing.T) {
	if os.Getenv("REFLOW_XDP_INTEGRATION") != "1" {
		t.Skip("set REFLOW_XDP_INTEGRATION=1 with BPF syscall access")
	}
	m, err := ebpf.NewMap(&ebpf.MapSpec{Type: ebpf.PerfEventArray, KeySize: 4, ValueSize: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	p, err := ebpf.NewProgram(&ebpf.ProgramSpec{Type: ebpf.XDP, License: "GPL", Instructions: instructions(m.FD(), 128)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()
	packet := make([]byte, 64)
	packet[12], packet[13] = 8, 0
	result, output, err := p.Test(packet)
	if err != nil {
		t.Fatal(err)
	}
	if result != 2 || !bytes.Equal(packet, output) {
		t.Fatalf("XDP changed forwarding: result=%d packet=%x", result, output)
	}
}

func TestPassiveLoopbackCaptureIntegration(t *testing.T) {
	if os.Getenv("REFLOW_XDP_INTEGRATION") != "1" {
		t.Skip("set REFLOW_XDP_INTEGRATION=1 with BPF and network administration capabilities")
	}
	iface, err := net.InterfaceByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.SourceConfig{Network: "xdp", Capture: config.CaptureConfig{Interface: "lo", XDP: config.XDPConfig{Mode: "generic"}}}
	if err = config.NormalizeCapture(&cfg); err != nil {
		t.Fatal(err)
	}
	d := &driver{cfg: cfg, index: iface.Index}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	marker := []byte("reflow-passive-xdp-copy")
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
	forwarded := make(chan bool, 1)
	go func() {
		buf := make([]byte, 128)
		_ = listener.SetReadDeadline(time.Now().Add(5 * time.Second))
		n, _, readErr := listener.ReadFrom(buf)
		forwarded <- readErr == nil && bytes.Equal(buf[:n], marker)
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err = <-done:
			if err != io.EOF {
				t.Fatalf("XDP capture: %v", err)
			}
			if !<-forwarded {
				t.Fatal("XDP prevented ordinary UDP delivery")
			}
			return
		case <-ctx.Done():
			t.Fatal("no XDP packet copy received")
		case <-ticker.C:
			if _, err = writer.Write(marker); err != nil {
				t.Fatal(err)
			}
		}
	}
}
