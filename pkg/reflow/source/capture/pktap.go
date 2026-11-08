package capture

import (
	"bytes"
	"encoding/binary"
)

const pktapFlagV2 = 0x00080000

func pktapCString(data []byte) string {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	return string(data)
}

// NormalizePktap strips Apple packet-tap metadata and preserves its inner link
// type. Incomplete framing stays untouched so downstream decoding sees the raw
// bytes; this function does not validate the captured packet contents.
func NormalizePktap(frame Frame) Frame {
	data := frame.Data
	if len(data) < 40 {
		return frame
	}
	flags := binary.LittleEndian.Uint32(data[36:])
	header := 0
	fields := map[string]any{"pktap_flags": flags}
	var name string
	if flags&pktapFlagV2 != 0 {
		header = int(data[0])
		if header < 40 || header > len(data) {
			return frame
		}
		frame.LinkType = uint32(binary.LittleEndian.Uint16(data[6:]))
		if offset := int(data[3]); offset > 0 && offset < header {
			name = pktapCString(data[offset:header])
		}
		fields["pktap_pid"] = int32(binary.LittleEndian.Uint32(data[28:]))
		fields["pktap_effective_pid"] = int32(binary.LittleEndian.Uint32(data[32:]))
		if offset := int(data[4]); offset > 0 && offset < header {
			fields["pktap_process"] = pktapCString(data[offset:header])
		}
		fields["pktap_version"] = 2
	} else {
		header = int(binary.LittleEndian.Uint32(data))
		if header < 40 || header > len(data) {
			return frame
		}
		frame.LinkType = binary.LittleEndian.Uint32(data[8:])
		name = pktapCString(data[12:36])
		if header >= 76 {
			fields["pktap_pid"] = int32(binary.LittleEndian.Uint32(data[52:]))
			fields["pktap_process"] = pktapCString(data[56:73])
		}
		fields["pktap_version"] = 1
	}
	if frame.LinkType == 12 {
		frame.LinkType = 101
	}
	frame.Data = data[header:]
	frame.CaptureLength = len(frame.Data)
	if frame.WireLength >= header {
		frame.WireLength -= header
	} else {
		frame.WireLength = 0
	}
	frame.Interface = name
	frame.InterfaceIndex = 0
	switch flags & 3 {
	case 1:
		frame.Direction = "ingress"
	case 2:
		frame.Direction = "egress"
	}
	if frame.Fields == nil {
		frame.Fields = make(map[string]any)
	}
	for key, value := range fields {
		frame.Fields[key] = value
	}
	return frame
}
