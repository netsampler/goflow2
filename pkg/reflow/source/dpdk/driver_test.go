//go:build linux && cgo && reflow_dpdk && !reflow_pcap

package dpdk

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

// The null PMD generates RX frames without hardware, hugepages or a kernel
// driver. Opt in on SDK-equipped Linux hosts to exercise EAL and buffer lifetime.
func TestNullPMDCapture(t *testing.T) {
	if os.Getenv("REFLOW_TEST_DPDK") != "1" {
		t.Skip("set REFLOW_TEST_DPDK=1 on a DPDK SDK host")
	}
	var cpus unix.CPUSet
	if err := unix.SchedGetaffinity(0, &cpus); err != nil {
		t.Fatal(err)
	}
	cpu := 0
	for !cpus.IsSet(cpu) {
		cpu++
	}
	cfg := config.SourceConfig{Network: "dpdk", Type: "bytes", Capture: config.CaptureConfig{
		SnapLen: 32,
		DPDK: config.DPDKConfig{Port: "net_null0", EALArgs: []string{
			"--lcores=0@" + strconv.Itoa(cpu), "--no-huge", "--no-pci", "--no-shconf", "--vdev=net_null0,size=64",
		}},
	}}
	src, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := Cleanup(); err != nil {
			t.Error(err)
		}
	}()
	defer func() { _ = src.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var data []byte
	err = src.Start(ctx, func(evt *event.Event) error {
		if evt.Kind == "control" {
			return nil
		}
		data = evt.Payload.([]byte)
		if len(data) != 32 || evt.Fields["wire_length"] != 64 {
			t.Fatalf("unexpected PMD frame: length=%d fields=%v", len(data), evt.Fields)
		}
		return io.EOF
	})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("capture did not emit the generated frame: %v", err)
	}
	if len(data) != 32 {
		t.Fatal("missing captured bytes")
	}
	// The retained data remains owned by Go after the mbuf is recycled and the
	// port is closed by Run's deferred cleanup.
	for _, b := range data {
		if b != 0 {
			t.Fatalf("unexpected null PMD bytes: %x", data)
		}
	}
}
