package bpf

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

// readRecords uses the kernel-provided header length, which may exceed sizeof
// bpf_hdr and varies between the BSDs. Packet contents are never validated here.
func readRecords(buf []byte, tsSize, alignment int, emit func(capture.Frame) error) error {
	for len(buf) > 0 {
		if len(buf) < tsSize+10 {
			return fmt.Errorf("truncated BPF record header")
		}
		var sec, usec int64
		if tsSize == 8 {
			sec = int64(binary.NativeEndian.Uint32(buf))
			usec = int64(binary.NativeEndian.Uint32(buf[4:]))
		} else {
			sec = int64(binary.NativeEndian.Uint64(buf))
			usec = int64(binary.NativeEndian.Uint64(buf[8:]))
		}
		caplen := int(binary.NativeEndian.Uint32(buf[tsSize:]))
		wire := int(binary.NativeEndian.Uint32(buf[tsSize+4:]))
		hlen := int(binary.NativeEndian.Uint16(buf[tsSize+8:]))
		if hlen < tsSize+10 || hlen > len(buf) || caplen > len(buf)-hlen {
			return fmt.Errorf("truncated BPF capture record")
		}
		if err := emit(capture.Frame{Data: buf[hlen : hlen+caplen], Timestamp: time.Unix(sec, usec*1000).UTC(), CaptureLength: caplen, WireLength: wire}); err != nil {
			return err
		}
		consumed := (hlen + caplen + alignment - 1) & ^(alignment - 1)
		if consumed >= len(buf) {
			return nil
		}
		buf = buf[consumed:]
	}
	return nil
}
