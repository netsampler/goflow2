package capture

import (
	"encoding/binary"
	"fmt"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// NormalizeFrame preserves engine lengths while translating capture framing to
// the Ethernet or raw-IP payload expected by the shared packet decoder.
func NormalizeFrame(f Frame) Frame {
	if f.LinkType == 149 || f.LinkType == 258 {
		f = NormalizePktap(f)
		if f.LinkType == 149 || f.LinkType == 258 {
			return f
		}
	}
	if f.Fields == nil {
		f.Fields = make(map[string]any)
	}
	f.Fields["pcap_link_type"] = f.LinkType
	name := fmt.Sprintf("LinkType(%d)", f.LinkType)
	if f.LinkType <= 255 {
		name = layers.LinkType(f.LinkType).String()
	}
	if f.LinkType == 276 {
		name = "Linux SLL2"
	}
	f.Fields["pcap_link_type_name"] = name
	protocol := uint32(0)
	switch f.LinkType {
	case 1:
		protocol = 1
	case 101, 12:
		if len(f.Data) > 0 {
			switch f.Data[0] >> 4 {
			case 4:
				protocol = 11
			case 6:
				protocol = 12
			}
		}
	case 228:
		protocol = 11
	case 229:
		protocol = 12
	case 0, 108:
		var loop layers.Loopback
		if loop.DecodeFromBytes(f.Data, gopacket.NilDecodeFeedback) == nil {
			switch loop.NextLayerType() {
			case layers.LayerTypeIPv4:
				protocol = 11
			case layers.LayerTypeIPv6:
				protocol = 12
			}
			if protocol != 0 {
				f.Data = loop.Payload
				f.Fields["pcap_cooked_header_length"] = 4
			}
		}
	case 113, 276:
		header := 16
		offset := 14
		addr := 6
		if f.LinkType == 276 {
			header = 20
			offset = 0
			addr = 12
		}
		if len(f.Data) < header {
			break
		}
		etherType := binary.BigEndian.Uint16(f.Data[offset : offset+2])
		packetType := uint8(binary.BigEndian.Uint16(f.Data[:2]))
		if f.LinkType == 276 {
			packetType = f.Data[10]
			f.InterfaceIndex = int(binary.BigEndian.Uint32(f.Data[4:8]))
			f.Fields["linux_sll_ifindex"] = uint32(f.InterfaceIndex)
		}
		names := []string{"host", "broadcast", "multicast", "otherhost", "outgoing", "loopback", "fastroute"}
		if int(packetType) < len(names) {
			f.PacketType = names[packetType]
		} else {
			f.PacketType = fmt.Sprintf("unknown(%d)", packetType)
		}
		f.Fields["capture_packet_type"] = f.PacketType
		switch packetType {
		case 4:
			f.Direction = "egress"
			f.Fields["output_if"] = uint32(f.InterfaceIndex)
		case 5:
			f.Direction = "ingress"
			f.Fields["input_if"] = uint32(f.InterfaceIndex)
			f.Fields["output_if"] = uint32(f.InterfaceIndex)
		default:
			f.Direction = "ingress"
			f.Fields["input_if"] = uint32(f.InterfaceIndex)
		}
		ethernet := make([]byte, 14+len(f.Data)-header)
		copy(ethernet[6:12], f.Data[addr:addr+6])
		binary.BigEndian.PutUint16(ethernet[12:14], etherType)
		copy(ethernet[14:], f.Data[header:])
		f.Data = ethernet
		protocol = 1
		f.Fields["linux_sll_protocol"] = uint32(etherType)
		f.Fields["pcap_cooked_header_length"] = header
	}
	if protocol != 0 {
		f.Fields["header_protocol"] = protocol
		f.Fields["protocol"] = protocol
	}
	return f
}
