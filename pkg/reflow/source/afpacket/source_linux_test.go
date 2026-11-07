//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package afpacket

import (
	"encoding/binary"
	"testing"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"golang.org/x/sys/unix"
)

func TestRingRecordVLAN(t *testing.T) {
	block := make([]byte, 256)
	binary.NativeEndian.PutUint32(block[12:], 1)
	binary.NativeEndian.PutUint32(block[16:], 48)
	h := block[48:]
	binary.NativeEndian.PutUint32(h[4:], 1700000000)
	binary.NativeEndian.PutUint32(h[12:], 18)
	binary.NativeEndian.PutUint32(h[16:], 1510)
	binary.NativeEndian.PutUint32(h[20:], unix.TP_STATUS_VLAN_VALID)
	binary.NativeEndian.PutUint16(h[24:], 80)
	binary.NativeEndian.PutUint32(h[32:], 123)
	binary.NativeEndian.PutUint32(h[52:], 7)
	copy(h[80:], []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 8, 0, 0x45, 0, 0, 20})
	d := &driver{cfg: config.SourceConfig{Capture: config.CaptureConfig{Direction: "both", Interface: "eth0"}}}
	var frames []capture.Frame
	if err := d.readBlock(block, 9, func(f capture.Frame) error { frames = append(frames, f); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames %d", len(frames))
	}
	f := frames[0]
	if f.InterfaceIndex != 7 || f.CaptureLength != 22 || f.WireLength != 1514 || f.Drops != 9 || binary.BigEndian.Uint16(f.Data[12:]) != 0x8100 || binary.BigEndian.Uint16(f.Data[14:]) != 123 {
		t.Fatalf("ring frame %+v", f)
	}
}
