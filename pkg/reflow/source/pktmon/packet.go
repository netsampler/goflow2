// Package pktmon receives packets from Windows Packet Monitor streaming APIs.
package pktmon

import (
	"encoding/binary"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

// decodePacket consumes the documented PACKETMONITOR_STREAM_METADATA ABI.
// There is no original wire length in that ABI; do not invent one when the
// packet may have been truncated by the streaming API.
func decodePacket(data, meta []byte) capture.Frame {
	f := capture.Frame{Data: data, CaptureLength: len(data), LinkType: ^uint32(0)}
	if len(meta) < 48 {
		return f
	}
	direction := binary.LittleEndian.Uint16(meta[12:])
	switch direction {
	case 1, 3, 5:
		f.Direction = "ingress"
	case 2, 4, 6:
		f.Direction = "egress"
	}
	switch binary.LittleEndian.Uint16(meta[14:]) {
	case 1:
		f.LinkType = 1
	case 2:
		f.LinkType = 105
	case 3:
		f.LinkType = 101
	}
	ticks := int64(binary.LittleEndian.Uint64(meta[40:]))
	if ticks > 0 {
		f.Timestamp = time.Unix(ticks/10000000-11644473600, (ticks%10000000)*100).UTC()
	}
	f.Fields = map[string]any{
		"pktmon_group_id":      binary.LittleEndian.Uint64(meta),
		"pktmon_component_id":  uint32(binary.LittleEndian.Uint16(meta[16:])),
		"pktmon_edge_id":       uint32(binary.LittleEndian.Uint16(meta[18:])),
		"pktmon_drop_reason":   binary.LittleEndian.Uint32(meta[24:]),
		"pktmon_drop_location": binary.LittleEndian.Uint32(meta[28:]),
	}
	return f
}
