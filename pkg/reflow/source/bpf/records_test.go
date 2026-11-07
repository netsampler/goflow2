package bpf

import (
	"encoding/binary"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"testing"
)

func TestBatchedRecords(t *testing.T) {
	for _, ts := range []int{8, 16} {
		hlen := ts + 12
		record := make([]byte, hlen+4)
		if ts == 8 {
			binary.NativeEndian.PutUint32(record, 123)
			binary.NativeEndian.PutUint32(record[4:], 456)
		} else {
			binary.NativeEndian.PutUint64(record, 123)
			binary.NativeEndian.PutUint64(record[8:], 456)
		}
		binary.NativeEndian.PutUint32(record[ts:], 4)
		binary.NativeEndian.PutUint32(record[ts+4:], 60)
		binary.NativeEndian.PutUint16(record[ts+8:], uint16(hlen))
		copy(record[hlen:], []byte{1, 2, 3, 4})
		count := 0
		if err := readRecords(append(record, record...), ts, 4, func(f capture.Frame) error {
			count++
			if len(f.Data) != 4 || f.WireLength != 60 || f.Timestamp.Unix() != 123 || f.Timestamp.Nanosecond() != 456000 {
				t.Fatalf("unexpected frame: %#v", f)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("got %d records", count)
		}
	}
}
