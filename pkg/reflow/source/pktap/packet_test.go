package pktap

import (
	"bytes"
	"encoding/binary"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"testing"
)

func TestPacketTapVersions(t *testing.T) {
	payload := []byte{0x45, 0, 0, 20}
	for _, version := range []int{1, 2} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			header := make([]byte, 80)
			if version == 1 {
				binary.LittleEndian.PutUint32(header, 80)
				binary.LittleEndian.PutUint32(header[4:], 1)
				binary.LittleEndian.PutUint32(header[8:], 12)
				copy(header[12:], "lo0")
				binary.LittleEndian.PutUint32(header[36:], 1)
				binary.LittleEndian.PutUint32(header[52:], 42)
				copy(header[56:], "sender")
			} else {
				header[0] = 80
				header[3] = 40
				header[4] = 48
				binary.LittleEndian.PutUint16(header[6:], 12)
				binary.LittleEndian.PutUint32(header[28:], 42)
				binary.LittleEndian.PutUint32(header[36:], flagV2|1)
				copy(header[40:], "lo0")
				copy(header[48:], "sender")
			}
			frame := normalize(capture.Frame{Data: append(header, payload...), WireLength: 100, LinkType: 258})
			if !bytes.Equal(frame.Data, payload) || frame.WireLength != 20 || frame.LinkType != 101 || frame.Interface != "lo0" || frame.Direction != "ingress" || frame.Fields["pktap_pid"] != int32(42) || frame.Fields["pktap_process"] != "sender" {
				t.Fatalf("unexpected normalized frame: %#v", frame)
			}
		})
	}
}
