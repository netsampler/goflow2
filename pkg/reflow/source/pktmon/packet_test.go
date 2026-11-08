package pktmon

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestPacketMetadata(t *testing.T) {
	meta := make([]byte, 48)
	binary.LittleEndian.PutUint16(meta[12:], 3)
	binary.LittleEndian.PutUint16(meta[14:], 1)
	binary.LittleEndian.PutUint16(meta[16:], 7)
	want := time.Unix(123, 456000000).UTC()
	ticks := (want.Unix()+11644473600)*10000000 + 4560000
	binary.LittleEndian.PutUint64(meta[40:], uint64(ticks))
	f := decodePacket([]byte{1, 2, 3}, meta)
	if f.Direction != "ingress" || f.LinkType != 1 || f.CaptureLength != 3 || f.WireLength != 0 || !f.Timestamp.Equal(want) || f.Fields["pktmon_component_id"] != uint32(7) {
		t.Fatalf("unexpected packet metadata: %#v", f)
	}
}
