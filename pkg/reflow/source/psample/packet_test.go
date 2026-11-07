package psample

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/netsampler/goflow2/v3/pkg/reflow/source/netlinkcapture"
)

func TestSampleMetadataAndSequence(t *testing.T) {
	d := newDecoder([]uint32{1}, map[string]string{"9001": "7"})
	packet := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 8, 0, 0x45, 0, 0, 20}
	for _, sequence := range []uint32{100, 103} {
		data := []byte{0, 1, 0, 0}
		for _, a := range []struct {
			k uint16
			v uint32
		}{{0, 9001}, {2, 1514}, {3, 1}, {4, sequence}, {5, 1000}} {
			data = append(data, netlinkcapture.Attribute(a.k, netlinkcapture.Uint32(a.v))...)
		}
		data = append(data, netlinkcapture.Attribute(6, packet)...)
		queue := make([]byte, 2)
		binary.NativeEndian.PutUint16(queue, 3)
		data = append(data, netlinkcapture.Attribute(10, queue)...)
		latency := make([]byte, 8)
		binary.NativeEndian.PutUint64(latency, 1234)
		data = append(data, netlinkcapture.Attribute(12, latency)...)
		f, ok, err := d.packet(data)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("sample omitted")
		}
		if !bytes.Equal(f.Data, packet) || f.WireLength != 1514 || f.InterfaceIndex != 7 || f.Direction != "ingress" || f.Sampling == nil || f.Sampling.Rate != 1000 {
			t.Fatalf("sample metadata: %+v", f)
		}
		if f.Fields["psample_output_queue"] != uint16(3) || f.Fields["psample_latency_ns"] != uint64(1234) {
			t.Fatalf("queue metadata: %+v", f.Fields)
		}
		want := uint64(0)
		if sequence == 103 {
			want = 2
		}
		if f.Fields["psample_sequence_gaps"] != want {
			t.Fatalf("sequence gaps: %+v", f.Fields)
		}
		if f.Drops != 0 {
			t.Fatal("sequence gaps were incorrectly reported as ASIC drops")
		}
	}
}
