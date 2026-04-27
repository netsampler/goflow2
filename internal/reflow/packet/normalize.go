package packet

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type NormalizeOptions struct {
	DisablePacketMapping bool
	BuildPseudoPacket    bool
	TruncatePacketBytes  int
	UsePayloadAsPacket   bool
	TruncatePayload      bool
	HeaderProtocol       uint32
	Extractors           []FeatureExtractor
}

type FeatureExtractor interface {
	Name() string
	Extract(*event.PacketModel, []byte) error
}

// NormalizeEvent applies the shared packet-field normalization used by raw
// packet sources and packet-carrying decoded flow records such as sFlow sampled
// headers.
func NormalizeEvent(evt *event.Event, opts NormalizeOptions) error {
	if evt == nil {
		return fmt.Errorf("normalize packet event: nil event")
	}

	fields := ensureFields(evt, 16)
	if opts.UsePayloadAsPacket {
		payload, ok := evt.Payload.([]byte)
		if !ok || len(payload) == 0 {
			return fmt.Errorf("normalize packet event: missing payload bytes")
		}
		fields["header_data"] = append([]byte(nil), payload...)
	}

	headerData := bytesField(fields, "header_data")
	if len(headerData) == 0 {
		ensurePseudoPacket(evt, fields, opts.BuildPseudoPacket)
		headerData = bytesField(fields, "header_data")
	}
	if len(headerData) == 0 {
		return nil
	}

	if evt.ReceivedAt.IsZero() {
		evt.ReceivedAt = time.Now().UTC()
	}
	if _, ok := fields["record_kind"]; !ok {
		fields["record_kind"] = "packet"
	}
	if fieldUint32(fields, "frame_length") == 0 {
		fields["frame_length"] = uint32(len(headerData))
	}
	if fieldUint32(fields, "original_length") == 0 {
		fields["original_length"] = uint32(len(headerData))
	}
	if _, ok := fields["bytes"]; !ok {
		fields["bytes"] = int64(fieldUint32(fields, "frame_length"))
	}
	if _, ok := fields["packets"]; !ok {
		fields["packets"] = int64(1)
	}
	if _, ok := fields["start_time_unix"]; !ok {
		fields["start_time_unix"] = evt.ReceivedAt.UnixMilli()
	}
	if _, ok := fields["end_time_unix"]; !ok {
		fields["end_time_unix"] = evt.ReceivedAt.UnixMilli()
	}
	if opts.HeaderProtocol != 0 && fieldUint32(fields, "protocol") == 0 {
		fields["protocol"] = opts.HeaderProtocol
	}
	if opts.HeaderProtocol != 0 {
		if _, ok := fields["header_protocol_name"]; !ok {
			fields["header_protocol_name"] = sampledHeaderProtocolName(opts.HeaderProtocol)
		}
	}
	if evt.SFlow != nil && evt.SFlow.AgentIP == "" {
		evt.SFlow.AgentIP = fieldStringOrZero(fields, "agent_ip")
	}
	setDefaultInterfaces(evt, fields)

	if view, err := parsePacketViewWithProtocol(headerData, opts.HeaderProtocol); err == nil {
		evt.Packet = view.Model
		for _, extractor := range opts.Extractors {
			if extractor == nil || evt.Packet == nil {
				continue
			}
			if err := extractor.Extract(evt.Packet, headerData); err != nil {
				return fmt.Errorf("extract packet feature %q: %w", extractor.Name(), err)
			}
		}
		if !opts.DisablePacketMapping {
			applyPacketViewFields(fields, view)
		}
	}
	ensurePseudoPacket(evt, fields, opts.BuildPseudoPacket)
	truncatePacketData(evt, fields, opts.TruncatePacketBytes, opts.TruncatePayload)
	return nil
}

func setDefaultInterfaces(evt *event.Event, fields map[string]any) {
	if fieldUint32(fields, "input_if") != 0 || fieldUint32(fields, "output_if") != 0 {
		return
	}
	if evt.Source.CaptureInterfaceIndex <= 0 {
		return
	}
	ifIndex := uint32(evt.Source.CaptureInterfaceIndex)
	fields["input_if"] = ifIndex
	fields["output_if"] = ifIndex
}

func truncatePacketData(evt *event.Event, fields map[string]any, maxBytes int, truncatePayload bool) {
	if maxBytes <= 0 {
		return
	}
	headerData := bytesField(fields, "header_data")
	if len(headerData) > maxBytes {
		fields["header_data"] = append([]byte(nil), headerData[:maxBytes]...)
	}
	if !truncatePayload {
		return
	}
	if payload, ok := evt.Payload.([]byte); ok && len(payload) > maxBytes {
		evt.Payload = append([]byte(nil), payload[:maxBytes]...)
	}
}

func bytesField(fields map[string]any, key string) []byte {
	if fields == nil {
		return nil
	}
	val, ok := fields[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		return nil
	}
}

func ensurePseudoPacket(evt *event.Event, fields map[string]any, enabled bool) {
	if !enabled {
		return
	}
	if len(bytesField(fields, "header_data")) > 0 {
		return
	}
	headerData, ok := buildPseudoPacket(evt, fields)
	if !ok {
		return
	}
	fields["header_data"] = headerData
	if fieldUint32(fields, "frame_length") == 0 {
		fields["frame_length"] = uint32(len(headerData))
	}
	if fieldUint32(fields, "original_length") == 0 {
		fields["original_length"] = uint32(len(headerData))
	}
	if fieldUint32(fields, "protocol") == 0 {
		fields["protocol"] = uint32(1)
	}
}

func buildPseudoPacket(evt *event.Event, fields map[string]any) ([]byte, bool) {
	model := evt.Packet
	if model == nil || len(model.Layers) == 0 {
		model = pseudoPacketModelFromFields(fields)
	}
	if model == nil || len(model.Layers) == 0 {
		return nil, false
	}
	data, err := encodePacketModel(model)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	if evt != nil && evt.Packet == nil {
		evt.Packet = model
	}
	return data, true
}

func pseudoPacketModelFromFields(fields map[string]any) *event.PacketModel {
	srcAddrStr := fieldStringOrZero(fields, "src_addr")
	dstAddrStr := fieldStringOrZero(fields, "dst_addr")
	if srcAddrStr == "" || dstAddrStr == "" {
		return nil
	}
	srcAddr, err := netip.ParseAddr(srcAddrStr)
	if err != nil {
		return nil
	}
	dstAddr, err := netip.ParseAddr(dstAddrStr)
	if err != nil {
		return nil
	}

	model := &event.PacketModel{
		Features: make(map[string]event.FeatureValue),
	}

	dstMAC := fieldStringOrZero(fields, "dst_mac")
	if dstMAC == "" {
		dstMAC = "00:00:00:00:00:00"
	}
	srcMAC := fieldStringOrZero(fields, "src_mac")
	if srcMAC == "" {
		srcMAC = "00:00:00:00:00:00"
	}
	model.Layers = append(model.Layers, event.LayerSpec{
		Kind: "ethernet",
		Ethernet: &event.EthernetLayer{
			SrcMAC: srcMAC,
			DstMAC: dstMAC,
		},
	})

	appendVLANLayers(model, fields)
	appendMPLSLayers(model, fields)
	appendPPPoELayer(model, fields)

	tunnelType := fieldStringOrZero(fields, "tunnel_type")
	if outerSrc := fieldStringOrZero(fields, "outer_src_addr"); outerSrc != "" {
		outerDst := fieldStringOrZero(fields, "outer_dst_addr")
		outerProto := fieldUint32(fields, "outer_proto")
		appendPseudoIPLayer(model, mustParseAddr(outerSrc), mustParseAddr(outerDst), outerProto)
		appendPseudoTunnelLayers(model, tunnelType, fieldUint32(fields, "outer_src_port"), fieldUint32(fields, "outer_dst_port"), srcAddr, dstAddr)
	}

	appendPseudoIPLayer(model, srcAddr, dstAddr, fieldUint32(fields, "proto"))
	appendPseudoTransportLayer(model, fieldUint32(fields, "proto"), fieldUint32(fields, "src_port"), fieldUint32(fields, "dst_port"))

	if frameLen := fieldUint32(fields, "original_length"); frameLen != 0 {
		model.Features["target_wire_length"] = event.FeatureUint64(uint64(frameLen))
	}
	return model
}

func appendVLANLayers(model *event.PacketModel, fields map[string]any) {
	if model == nil {
		return
	}
	if vals, ok := fields["vlan_ids"].([]uint32); ok && len(vals) > 0 {
		for _, id := range vals {
			model.Layers = append(model.Layers, event.LayerSpec{
				Kind: "dot1q",
				VLAN: &event.VLANLayer{ID: uint16(id), TPID: 0x8100},
			})
		}
		return
	}
	if id := fieldUint32(fields, "vlan_id"); id != 0 {
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "dot1q",
			VLAN: &event.VLANLayer{ID: uint16(id), TPID: 0x8100},
		})
	}
}

func appendMPLSLayers(model *event.PacketModel, fields map[string]any) {
	if model == nil {
		return
	}
	if label := fieldUint32(fields, "mpls_label"); label != 0 {
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "mpls",
			MPLS: &event.MPLSLayer{
				Label: event.MPLSLabel{
					Label: label,
					BOS:   true,
					TTL:   64,
				},
			},
		})
	}
}

func appendPPPoELayer(model *event.PacketModel, fields map[string]any) {
	if model == nil {
		return
	}
	sessionID := fieldUint32(fields, "pppoe_session_id")
	if sessionID == 0 {
		return
	}
	proto := uint16(0x0021)
	if strings.Contains(fieldStringOrZero(fields, "src_addr"), ":") {
		proto = 0x0057
	}
	model.Layers = append(model.Layers, event.LayerSpec{
		Kind: "pppoe",
		PPPoE: &event.PPPoELayer{
			SessionID: uint16(sessionID),
			Protocol:  proto,
		},
	})
}

func appendPseudoTunnelLayers(model *event.PacketModel, tunnelType string, srcPort, dstPort uint32, innerSrc, innerDst netip.Addr) {
	if model == nil {
		return
	}
	switch tunnelType {
	case "gre":
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "gre",
			GRE:  &event.GRELayer{Protocol: pseudoInnerEtherType(innerSrc, innerDst)},
		})
	case "vxlan":
		model.Layers = append(model.Layers,
			event.LayerSpec{Kind: "udp", UDP: &event.UDPLayer{SrcPort: uint16(srcPort), DstPort: 4789}},
			event.LayerSpec{Kind: "vxlan", VXLAN: &event.VXLANLayer{}},
			event.LayerSpec{Kind: "ethernet", Ethernet: &event.EthernetLayer{SrcMAC: "00:00:00:00:00:00", DstMAC: "00:00:00:00:00:00"}},
		)
	case "geneve":
		model.Layers = append(model.Layers,
			event.LayerSpec{Kind: "udp", UDP: &event.UDPLayer{SrcPort: uint16(srcPort), DstPort: 6081}},
			event.LayerSpec{Kind: "geneve", Geneve: &event.GeneveLayer{Protocol: pseudoInnerEtherType(innerSrc, innerDst)}},
			event.LayerSpec{Kind: "ethernet", Ethernet: &event.EthernetLayer{SrcMAC: "00:00:00:00:00:00", DstMAC: "00:00:00:00:00:00"}},
		)
	}
}

func appendPseudoIPLayer(model *event.PacketModel, srcAddr, dstAddr netip.Addr, proto uint32) {
	if srcAddr.Is4() && dstAddr.Is4() {
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "ipv4",
			IPv4: &event.IPv4Layer{
				SrcAddr:  srcAddr,
				DstAddr:  dstAddr,
				Protocol: uint8(proto),
				TTL:      64,
			},
		})
		return
	}
	if srcAddr.Is6() && dstAddr.Is6() {
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "ipv6",
			IPv6: &event.IPv6Layer{
				SrcAddr:    srcAddr,
				DstAddr:    dstAddr,
				NextHeader: uint8(proto),
				HopLimit:   64,
			},
		})
	}
}

func appendPseudoTransportLayer(model *event.PacketModel, proto, srcPort, dstPort uint32) {
	switch proto {
	case 6:
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "tcp",
			TCP: &event.TCPLayer{
				SrcPort: uint16(srcPort),
				DstPort: uint16(dstPort),
				Flags:   0x02,
				Window:  65535,
			},
		})
	case 17:
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "udp",
			UDP: &event.UDPLayer{
				SrcPort: uint16(srcPort),
				DstPort: uint16(dstPort),
			},
		})
	case 1, 58:
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "icmp",
			ICMP: &event.ICMPLayer{},
		})
	}
}

func pseudoInnerEtherType(srcAddr, dstAddr netip.Addr) uint16 {
	if srcAddr.Is6() && dstAddr.Is6() {
		return 0x86dd
	}
	return 0x0800
}

func encodePacketModel(model *event.PacketModel) ([]byte, error) {
	if model == nil || len(model.Layers) == 0 {
		return nil, fmt.Errorf("empty packet model")
	}
	payload := []byte(nil)
	for i := len(model.Layers) - 1; i >= 0; i-- {
		layer := model.Layers[i]
		var err error
		payload, err = prependLayer(layer, payload)
		if err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func prependLayer(layer event.LayerSpec, payload []byte) ([]byte, error) {
	switch layer.Kind {
	case "ethernet":
		return prependEthernet(layer.Ethernet, payload)
	case "dot1q":
		return prependVLAN(layer.VLAN, payload)
	case "mpls":
		return prependMPLS(layer.MPLS, payload)
	case "pppoe":
		return prependPPPoE(layer.PPPoE, payload)
	case "ipv4":
		return prependIPv4(layer.IPv4, payload)
	case "ipv6":
		return prependIPv6(layer.IPv6, payload)
	case "gre":
		return prependGRE(layer.GRE, payload)
	case "udp":
		return prependUDP(layer.UDP, payload)
	case "tcp":
		return prependTCP(layer.TCP, payload)
	case "vxlan":
		return prependVXLAN(layer.VXLAN, payload)
	case "geneve":
		return prependGeneve(layer.Geneve, payload)
	case "icmp", "icmpv6":
		return prependICMP(layer.ICMP, payload), nil
	case "payload":
		return prependPayload(layer.Payload, payload), nil
	default:
		return payload, nil
	}
}

func prependEthernet(layer *event.EthernetLayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.EthernetLayer{}
	}
	out := make([]byte, 14+len(payload))
	copy(out[0:6], parseMACOrZero(layer.DstMAC))
	copy(out[6:12], parseMACOrZero(layer.SrcMAC))
	etherType := inferEtherType(layer.EtherType, payload)
	binary.BigEndian.PutUint16(out[12:14], uint16(etherType))
	copy(out[14:], payload)
	return out, nil
}

func prependVLAN(layer *event.VLANLayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.VLANLayer{TPID: 0x8100}
	}
	out := make([]byte, 4+len(payload))
	tci := uint16(layer.ID&0x0fff) | uint16(layer.PCP&0x7)<<13
	if layer.DEI {
		tci |= 1 << 12
	}
	binary.BigEndian.PutUint16(out[0:2], tci)
	binary.BigEndian.PutUint16(out[2:4], uint16(inferEtherType(0, payload)))
	copy(out[4:], payload)
	return out, nil
}

func prependMPLS(layer *event.MPLSLayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.MPLSLayer{Label: event.MPLSLabel{BOS: true, TTL: 64}}
	}
	out := make([]byte, 4+len(payload))
	label := (layer.Label.Label & 0xfffff) << 12
	label |= uint32(layer.Label.TC&0x7) << 9
	if layer.Label.BOS {
		label |= 1 << 8
	}
	label |= uint32(layer.Label.TTL)
	binary.BigEndian.PutUint32(out[0:4], label)
	copy(out[4:], payload)
	return out, nil
}

func prependPPPoE(layer *event.PPPoELayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.PPPoELayer{Protocol: 0x0021}
	}
	out := make([]byte, 8+len(payload))
	out[0] = 0x11
	out[1] = 0x00
	binary.BigEndian.PutUint16(out[2:4], layer.SessionID)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(payload)+2))
	binary.BigEndian.PutUint16(out[6:8], layer.Protocol)
	copy(out[8:], payload)
	return out, nil
}

func prependIPv4(layer *event.IPv4Layer, payload []byte) ([]byte, error) {
	if layer == nil {
		return nil, fmt.Errorf("missing ipv4 layer")
	}
	out := make([]byte, 20+len(payload))
	out[0] = 0x45
	out[1] = byte((layer.DSCP << 2) | (layer.ECN & 0x3))
	binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
	binary.BigEndian.PutUint16(out[4:6], layer.Identification)
	flagsFrag := uint16(layer.Flags&0x7)<<13 | (layer.FragmentOffset & 0x1fff)
	binary.BigEndian.PutUint16(out[6:8], flagsFrag)
	ttl := layer.TTL
	if ttl == 0 {
		ttl = 64
	}
	out[8] = ttl
	out[9] = layer.Protocol
	copy(out[12:16], layer.SrcAddr.AsSlice())
	copy(out[16:20], layer.DstAddr.AsSlice())
	copy(out[20:], payload)
	return out, nil
}

func prependIPv6(layer *event.IPv6Layer, payload []byte) ([]byte, error) {
	if layer == nil {
		return nil, fmt.Errorf("missing ipv6 layer")
	}
	out := make([]byte, 40+len(payload))
	out[0] = 0x60 | (layer.TrafficClass >> 4)
	out[1] = (layer.TrafficClass << 4) | byte((layer.FlowLabel>>16)&0x0f)
	out[2] = byte(layer.FlowLabel >> 8)
	out[3] = byte(layer.FlowLabel)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(payload)))
	out[6] = layer.NextHeader
	hopLimit := layer.HopLimit
	if hopLimit == 0 {
		hopLimit = 64
	}
	out[7] = hopLimit
	copy(out[8:24], layer.SrcAddr.AsSlice())
	copy(out[24:40], layer.DstAddr.AsSlice())
	copy(out[40:], payload)
	return out, nil
}

func prependGRE(layer *event.GRELayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.GRELayer{Protocol: uint16(inferEtherType(0, payload))}
	}
	offset := 4
	flags := uint16(0)
	if layer.Checksum {
		flags |= 0x8000
		offset += 4
	}
	if layer.Key {
		flags |= 0x2000
		offset += 4
	}
	if layer.Sequence {
		flags |= 0x1000
		offset += 4
	}
	out := make([]byte, offset+len(payload))
	binary.BigEndian.PutUint16(out[0:2], flags)
	binary.BigEndian.PutUint16(out[2:4], layer.Protocol)
	copy(out[offset:], payload)
	return out, nil
}

func prependUDP(layer *event.UDPLayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.UDPLayer{}
	}
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(out[0:2], layer.SrcPort)
	binary.BigEndian.PutUint16(out[2:4], layer.DstPort)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(out)))
	copy(out[8:], payload)
	return out, nil
}

func prependTCP(layer *event.TCPLayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.TCPLayer{Flags: 0x02, Window: 65535}
	}
	out := make([]byte, 20+len(payload))
	binary.BigEndian.PutUint16(out[0:2], layer.SrcPort)
	binary.BigEndian.PutUint16(out[2:4], layer.DstPort)
	binary.BigEndian.PutUint32(out[4:8], layer.Seq)
	binary.BigEndian.PutUint32(out[8:12], layer.Ack)
	out[12] = 0x50
	out[13] = layer.Flags
	window := layer.Window
	if window == 0 {
		window = 65535
	}
	binary.BigEndian.PutUint16(out[14:16], window)
	copy(out[20:], payload)
	return out, nil
}

func prependVXLAN(layer *event.VXLANLayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.VXLANLayer{}
	}
	out := make([]byte, 8+len(payload))
	out[0] = 0x08
	out[4] = byte(layer.VNI >> 16)
	out[5] = byte(layer.VNI >> 8)
	out[6] = byte(layer.VNI)
	copy(out[8:], payload)
	return out, nil
}

func prependGeneve(layer *event.GeneveLayer, payload []byte) ([]byte, error) {
	if layer == nil {
		layer = &event.GeneveLayer{Protocol: uint16(inferEtherType(0, payload))}
	}
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(out[2:4], layer.Protocol)
	out[4] = byte(layer.VNI >> 16)
	out[5] = byte(layer.VNI >> 8)
	out[6] = byte(layer.VNI)
	copy(out[8:], payload)
	return out, nil
}

func prependICMP(layer *event.ICMPLayer, payload []byte) []byte {
	if layer == nil {
		layer = &event.ICMPLayer{}
	}
	out := make([]byte, 8+len(payload))
	out[0] = layer.Type
	out[1] = layer.Code
	copy(out[8:], payload)
	return out
}

func prependPayload(layer *event.PayloadLayer, payload []byte) []byte {
	if layer == nil || layer.Length == 0 {
		return payload
	}
	if len(payload) >= int(layer.Length) {
		return payload
	}
	fill := make([]byte, int(layer.Length)-len(payload))
	switch layer.Pattern {
	case "repeat":
		for i := range fill {
			fill[i] = 0xaa
		}
	}
	return append(fill, payload...)
}

func inferEtherType(explicit uint32, payload []byte) uint32 {
	if explicit != 0 {
		return explicit
	}
	if len(payload) == 0 {
		return 0x0800
	}
	switch payload[0] >> 4 {
	case 4:
		return 0x0800
	case 6:
		return 0x86dd
	default:
		return 0x6558
	}
}

func parseMACOrZero(raw string) []byte {
	out := make([]byte, 6)
	if raw == "" {
		return out
	}
	var vals [6]byte
	if _, err := fmt.Sscanf(raw, "%02x:%02x:%02x:%02x:%02x:%02x", &vals[0], &vals[1], &vals[2], &vals[3], &vals[4], &vals[5]); err != nil {
		return out
	}
	copy(out, vals[:])
	return out
}

func mustParseAddr(raw string) netip.Addr {
	addr, _ := netip.ParseAddr(raw)
	return addr
}

func pseudoL4HeaderLen(proto uint32) int {
	switch proto {
	case 6:
		return 20
	case 17:
		return 8
	default:
		return 8
	}
}

func fillPseudoL4(buf []byte, proto, srcPort, dstPort uint32) {
	if len(buf) < 8 {
		return
	}
	binary.BigEndian.PutUint16(buf[0:2], uint16(srcPort))
	binary.BigEndian.PutUint16(buf[2:4], uint16(dstPort))
	switch proto {
	case 6:
		if len(buf) < 20 {
			return
		}
		buf[12] = 0x50
	case 17:
		binary.BigEndian.PutUint16(buf[4:6], uint16(len(buf)))
	default:
		binary.BigEndian.PutUint16(buf[4:6], uint16(len(buf)))
	}
}

type packetTuple struct {
	SrcAddr netip.Addr
	DstAddr netip.Addr
	Proto   uint32
	SrcPort uint32
	DstPort uint32
}

type packetView struct {
	Layers     []string
	SrcMAC     string
	DstMAC     string
	EtherType  uint32
	VLANIDs    []uint32
	TunnelType string
	Tuples     []packetTuple
	Model      *event.PacketModel
}

func (v *packetView) appendLayer(layer event.LayerSpec) {
	v.Layers = append(v.Layers, layer.Kind)
	if v.Model == nil {
		v.Model = &event.PacketModel{}
	}
	v.Model.Layers = append(v.Model.Layers, layer)
}

func applyPacketViewFields(fields map[string]any, view packetView) {
	if view.SrcMAC != "" {
		fields["src_mac"] = view.SrcMAC
	}
	if view.DstMAC != "" {
		fields["dst_mac"] = view.DstMAC
	}
	if view.EtherType != 0 {
		fields["ether_type"] = view.EtherType
	}
	if len(view.Layers) > 0 {
		fields["packet_layers"] = append([]string(nil), view.Layers...)
	}
	if view.Model != nil {
		fields["packet_ip_depth"] = uint32(packetIPDepth(view.Model))
	}
	if len(view.VLANIDs) > 0 {
		fields["vlan_ids"] = append([]uint32(nil), view.VLANIDs...)
		fields["vlan_id"] = view.VLANIDs[0]
	}
	if len(view.Tuples) > 0 {
		tuple := view.Tuples[len(view.Tuples)-1]
		fields["src_addr"] = tuple.SrcAddr.String()
		fields["dst_addr"] = tuple.DstAddr.String()
		fields["proto"] = tuple.Proto
		fields["proto_name"] = ipProtocolName(tuple.Proto)
		fields["src_port"] = tuple.SrcPort
		fields["dst_port"] = tuple.DstPort
	}
	if len(view.Tuples) > 1 {
		outer := view.Tuples[0]
		fields["outer_src_addr"] = outer.SrcAddr.String()
		fields["outer_dst_addr"] = outer.DstAddr.String()
		fields["outer_proto"] = outer.Proto
		fields["outer_proto_name"] = ipProtocolName(outer.Proto)
		fields["outer_src_port"] = outer.SrcPort
		fields["outer_dst_port"] = outer.DstPort
		fields["encap_depth"] = uint32(len(view.Tuples) - 1)
	}
	if view.TunnelType != "" {
		fields["tunnel_type"] = view.TunnelType
	}
}

func packetIPDepth(model *event.PacketModel) int {
	if model == nil {
		return 0
	}
	count := 0
	for _, layer := range model.Layers {
		if layer.IPv4 != nil || layer.IPv6 != nil {
			count++
		}
	}
	return count
}

func finalizePacketView(view packetView) packetView {
	if view.Model == nil {
		return view
	}
	if view.Model.Features == nil {
		view.Model.Features = make(map[string]event.FeatureValue)
	}
	layerFeatures := make([]event.FeatureValue, 0, len(view.Layers))
	for _, layer := range view.Layers {
		layerFeatures = append(layerFeatures, event.FeatureString(layer))
	}
	view.Model.Features["layer_kinds"] = event.FeatureList(layerFeatures...)
	view.Model.Features["ip_depth"] = event.FeatureUint64(uint64(packetIPDepth(view.Model)))
	if len(view.Tuples) > 1 {
		view.Model.Features["encap_depth"] = event.FeatureUint64(uint64(len(view.Tuples) - 1))
	}
	if view.TunnelType != "" {
		view.Model.Features["tunnel_type"] = event.FeatureString(view.TunnelType)
	}
	return view
}

func parsePacketTuple(data []byte) (packetTuple, error) {
	view, err := parsePacketView(data)
	if err != nil {
		return packetTuple{}, err
	}
	if len(view.Tuples) == 0 {
		return packetTuple{}, fmt.Errorf("no ip tuple found")
	}
	return view.Tuples[len(view.Tuples)-1], nil
}

func parsePacketView(data []byte) (packetView, error) {
	return parsePacketViewWithProtocol(data, 0)
}

func parsePacketViewWithProtocol(data []byte, protocol uint32) (packetView, error) {
	if len(data) == 0 {
		return packetView{}, fmt.Errorf("empty packet header")
	}
	view := packetView{
		Model: &event.PacketModel{
			Features: make(map[string]event.FeatureValue),
		},
	}
	offset := 0
	etherType := uint16(0)
	switch protocol {
	case 1:
		var err error
		offset, etherType, err = parseEthernet(data, &view, true)
		if err != nil {
			return packetView{}, err
		}
	case 11:
		etherType = 0x0800
	case 12:
		etherType = 0x86dd
	default:
		if len(data) >= 14 {
			var err error
			offset, etherType, err = parseEthernet(data, &view, true)
			if err != nil {
				return packetView{}, err
			}
			break
		}
		switch data[0] >> 4 {
		case 4, 6:
		default:
			return packetView{}, fmt.Errorf("truncated packet header")
		}
	}
	if len(data) <= offset {
		return packetView{}, fmt.Errorf("truncated packet header")
	}
	for {
		switch {
		case etherType == 0x0800 || data[offset]>>4 == 4:
			nextOffset, tuple, nextProto, err := parseIPv4Tuple(data[offset:])
			if err != nil {
				return packetView{}, err
			}
			view.appendLayer(event.LayerSpec{
				Kind: "ipv4",
				IPv4: &event.IPv4Layer{
					SrcAddr:        tuple.SrcAddr,
					DstAddr:        tuple.DstAddr,
					Protocol:       uint8(tuple.Proto),
					TTL:            data[offset+8],
					DSCP:           data[offset+1] >> 2,
					ECN:            data[offset+1] & 0x03,
					Identification: binary.BigEndian.Uint16(data[offset+4 : offset+6]),
					Flags:          uint8(binary.BigEndian.Uint16(data[offset+6:offset+8]) >> 13),
					FragmentOffset: binary.BigEndian.Uint16(data[offset+6:offset+8]) & 0x1fff,
				},
			})
			view.Tuples = append(view.Tuples, tuple)
			if nextProto == 47 {
				view.TunnelType = "gre"
				innerOffset, innerProto, err := parseGRE(data[offset+nextOffset:], &view)
				if err != nil {
					return view, nil
				}
				offset += nextOffset + innerOffset
				etherType = innerProto
				if etherType == 0x6558 {
					nextOffset, nextEtherType, err := parseEthernet(data[offset:], &view, false)
					if err != nil {
						return view, nil
					}
					offset += nextOffset
					etherType = nextEtherType
					continue
				}
				if etherType == 0x0800 || etherType == 0x86dd || etherType == 0x8847 || etherType == 0x8848 || etherType == 0x8864 {
					continue
				}
			}
			if tuple.Proto == 17 {
				if tunnel, innerOffset, innerEtherType, err := parseUDPTunnel(data[offset+nextOffset:], tuple, &view); err == nil {
					view.TunnelType = tunnel
					offset += nextOffset + innerOffset
					if innerEtherType == 0x6558 {
						nextOffset, nextEtherType, err := parseEthernet(data[offset:], &view, false)
						if err != nil {
							return view, nil
						}
						offset += nextOffset
						etherType = nextEtherType
						continue
					}
					etherType = innerEtherType
					if etherType == 0x0800 || etherType == 0x86dd || etherType == 0x8847 || etherType == 0x8848 || etherType == 0x8864 {
						continue
					}
				}
			}
			appendTransportLayer(&view, tuple.Proto)
			return finalizePacketView(view), nil
		case etherType == 0x86dd || data[offset]>>4 == 6:
			nextOffset, tuple, nextProto, err := parseIPv6Tuple(data[offset:])
			if err != nil {
				return packetView{}, err
			}
			view.appendLayer(event.LayerSpec{
				Kind: "ipv6",
				IPv6: &event.IPv6Layer{
					SrcAddr:      tuple.SrcAddr,
					DstAddr:      tuple.DstAddr,
					NextHeader:   uint8(tuple.Proto),
					HopLimit:     data[offset+7],
					TrafficClass: ((data[offset]&0x0f)<<4 | (data[offset+1] >> 4)),
					FlowLabel:    uint32(data[offset+1]&0x0f)<<16 | uint32(data[offset+2])<<8 | uint32(data[offset+3]),
				},
			})
			view.Tuples = append(view.Tuples, tuple)
			if nextProto == 47 {
				view.TunnelType = "gre"
				innerOffset, innerProto, err := parseGRE(data[offset+nextOffset:], &view)
				if err != nil {
					return view, nil
				}
				offset += nextOffset + innerOffset
				etherType = innerProto
				if etherType == 0x6558 {
					nextOffset, nextEtherType, err := parseEthernet(data[offset:], &view, false)
					if err != nil {
						return view, nil
					}
					offset += nextOffset
					etherType = nextEtherType
					continue
				}
				if etherType == 0x0800 || etherType == 0x86dd || etherType == 0x8847 || etherType == 0x8848 || etherType == 0x8864 {
					continue
				}
			}
			if tuple.Proto == 17 {
				if tunnel, innerOffset, innerEtherType, err := parseUDPTunnel(data[offset+nextOffset:], tuple, &view); err == nil {
					view.TunnelType = tunnel
					offset += nextOffset + innerOffset
					if innerEtherType == 0x6558 {
						nextOffset, nextEtherType, err := parseEthernet(data[offset:], &view, false)
						if err != nil {
							return view, nil
						}
						offset += nextOffset
						etherType = nextEtherType
						continue
					}
					etherType = innerEtherType
					if etherType == 0x0800 || etherType == 0x86dd || etherType == 0x8847 || etherType == 0x8848 || etherType == 0x8864 {
						continue
					}
				}
			}
			appendTransportLayer(&view, tuple.Proto)
			return finalizePacketView(view), nil
		case etherType == 0x8847 || etherType == 0x8848:
			view.TunnelType = "mpls"
			innerOffset, innerProto, err := parseMPLS(data[offset:], &view)
			if err != nil {
				return view, nil
			}
			offset += innerOffset
			etherType = innerProto
			continue
		case etherType == 0x8864:
			view.TunnelType = "pppoe"
			innerOffset, innerProto, err := parsePPPoE(data[offset:], &view)
			if err != nil {
				return view, nil
			}
			offset += innerOffset
			etherType = innerProto
			continue
		default:
			return packetView{}, fmt.Errorf("unsupported ip version")
		}
	}
}

func parseEthernet(data []byte, view *packetView, captureMAC bool) (int, uint16, error) {
	if len(data) < 14 {
		return 0, 0, fmt.Errorf("truncated ethernet header")
	}
	layer := event.LayerSpec{
		Kind:     "ethernet",
		Ethernet: &event.EthernetLayer{},
	}
	if captureMAC {
		view.DstMAC = formatMAC(data[0:6])
		view.SrcMAC = formatMAC(data[6:12])
		layer.Ethernet.DstMAC = view.DstMAC
		layer.Ethernet.SrcMAC = view.SrcMAC
	}
	etherType := binary.BigEndian.Uint16(data[12:14])
	layer.Ethernet.EtherType = uint32(etherType)
	view.appendLayer(layer)
	offset := 14
	for etherType == 0x8100 || etherType == 0x88a8 {
		if len(data) < offset+4 {
			return 0, 0, fmt.Errorf("truncated vlan header")
		}
		tci := binary.BigEndian.Uint16(data[offset : offset+2])
		view.VLANIDs = append(view.VLANIDs, uint32(tci&0x0fff))
		view.appendLayer(event.LayerSpec{
			Kind: "dot1q",
			VLAN: &event.VLANLayer{
				ID:   tci & 0x0fff,
				PCP:  uint8((tci >> 13) & 0x7),
				DEI:  ((tci >> 12) & 0x1) == 1,
				TPID: etherType,
			},
		})
		etherType = binary.BigEndian.Uint16(data[offset+2 : offset+4])
		offset += 4
	}
	if view.EtherType == 0 {
		view.EtherType = uint32(etherType)
	}
	return offset, etherType, nil
}

func parseIPv4Tuple(data []byte) (int, packetTuple, uint32, error) {
	if len(data) < 20 {
		return 0, packetTuple{}, 0, fmt.Errorf("truncated ipv4 header")
	}
	ihl := int(data[0]&0x0f) * 4
	if ihl < 20 || len(data) < ihl {
		return 0, packetTuple{}, 0, fmt.Errorf("invalid ipv4 header length")
	}
	src, ok := netip.AddrFromSlice(data[12:16])
	if !ok {
		return 0, packetTuple{}, 0, fmt.Errorf("invalid ipv4 source address")
	}
	dst, ok := netip.AddrFromSlice(data[16:20])
	if !ok {
		return 0, packetTuple{}, 0, fmt.Errorf("invalid ipv4 destination address")
	}
	tuple := packetTuple{
		SrcAddr: src,
		DstAddr: dst,
		Proto:   uint32(data[9]),
	}
	if len(data) >= ihl+4 && (tuple.Proto == 6 || tuple.Proto == 17) {
		tuple.SrcPort = uint32(uint16(data[ihl])<<8 | uint16(data[ihl+1]))
		tuple.DstPort = uint32(uint16(data[ihl+2])<<8 | uint16(data[ihl+3]))
	}
	return ihl, tuple, tuple.Proto, nil
}

func parseIPv6Tuple(data []byte) (int, packetTuple, uint32, error) {
	if len(data) < 40 {
		return 0, packetTuple{}, 0, fmt.Errorf("truncated ipv6 header")
	}
	src, ok := netip.AddrFromSlice(data[8:24])
	if !ok {
		return 0, packetTuple{}, 0, fmt.Errorf("invalid ipv6 source address")
	}
	dst, ok := netip.AddrFromSlice(data[24:40])
	if !ok {
		return 0, packetTuple{}, 0, fmt.Errorf("invalid ipv6 destination address")
	}
	nextHeader := data[6]
	offset := 40
	for {
		if !isIPv6ExtensionHeader(nextHeader) {
			break
		}
		if len(data) < offset+2 {
			return 0, packetTuple{}, 0, fmt.Errorf("truncated ipv6 extension header")
		}
		switch nextHeader {
		case 44:
			if len(data) < offset+8 {
				return 0, packetTuple{}, 0, fmt.Errorf("truncated ipv6 fragment header")
			}
			nextHeader = data[offset]
			offset += 8
		case 51:
			hdrLen := (int(data[offset+1]) + 2) * 4
			if len(data) < offset+hdrLen {
				return 0, packetTuple{}, 0, fmt.Errorf("truncated ipv6 authentication header")
			}
			nextHeader = data[offset]
			offset += hdrLen
		default:
			hdrLen := (int(data[offset+1]) + 1) * 8
			if len(data) < offset+hdrLen {
				return 0, packetTuple{}, 0, fmt.Errorf("truncated ipv6 extension header")
			}
			nextHeader = data[offset]
			offset += hdrLen
		}
	}
	tuple := packetTuple{
		SrcAddr: src,
		DstAddr: dst,
		Proto:   uint32(nextHeader),
	}
	if len(data) >= offset+4 && (tuple.Proto == 6 || tuple.Proto == 17) {
		tuple.SrcPort = uint32(uint16(data[offset])<<8 | uint16(data[offset+1]))
		tuple.DstPort = uint32(uint16(data[offset+2])<<8 | uint16(data[offset+3]))
	}
	return offset, tuple, tuple.Proto, nil
}

func parseGRE(data []byte, view *packetView) (int, uint16, error) {
	if len(data) < 4 {
		return 0, 0, fmt.Errorf("truncated gre header")
	}
	flags := binary.BigEndian.Uint16(data[0:2])
	proto := binary.BigEndian.Uint16(data[2:4])
	view.appendLayer(event.LayerSpec{
		Kind: "gre",
		GRE: &event.GRELayer{
			Protocol: proto,
			Checksum: flags&0x8000 != 0,
			Key:      flags&0x2000 != 0,
			Sequence: flags&0x1000 != 0,
		},
	})
	offset := 4
	if flags&0x8000 != 0 {
		offset += 4
	}
	if flags&0x2000 != 0 {
		offset += 4
	}
	if flags&0x1000 != 0 {
		offset += 4
	}
	if len(data) < offset {
		return 0, 0, fmt.Errorf("truncated gre optional fields")
	}
	return offset, proto, nil
}

func parseUDPTunnel(data []byte, tuple packetTuple, view *packetView) (string, int, uint16, error) {
	if len(data) < 8 {
		return "", 0, 0, fmt.Errorf("truncated udp header")
	}
	switch {
	case tuple.DstPort == 4789 || tuple.SrcPort == 4789:
		if len(data) < 16 {
			return "", 0, 0, fmt.Errorf("truncated vxlan header")
		}
		view.appendLayer(event.LayerSpec{
			Kind: "vxlan",
			VXLAN: &event.VXLANLayer{
				VNI: uint32(data[12])<<16 | uint32(data[13])<<8 | uint32(data[14]),
			},
		})
		return "vxlan", 16, 0x6558, nil
	case tuple.DstPort == 6081 || tuple.SrcPort == 6081:
		if len(data) < 16 {
			return "", 0, 0, fmt.Errorf("truncated geneve header")
		}
		optLen := int((data[8] & 0x3f) * 4)
		proto := binary.BigEndian.Uint16(data[10:12])
		offset := 8 + 8 + optLen
		if len(data) < offset {
			return "", 0, 0, fmt.Errorf("truncated geneve options")
		}
		view.appendLayer(event.LayerSpec{
			Kind: "geneve",
			Geneve: &event.GeneveLayer{
				VNI:      uint32(data[12])<<16 | uint32(data[13])<<8 | uint32(data[14]),
				Protocol: proto,
			},
		})
		return "geneve", offset, proto, nil
	default:
		return "", 0, 0, fmt.Errorf("not a supported udp tunnel")
	}
}

func parseMPLS(data []byte, view *packetView) (int, uint16, error) {
	if len(data) < 4 {
		return 0, 0, fmt.Errorf("truncated mpls header")
	}
	offset := 0
	for {
		if len(data) < offset+4 {
			return 0, 0, fmt.Errorf("truncated mpls label stack")
		}
		label := binary.BigEndian.Uint32(data[offset : offset+4])
		view.appendLayer(event.LayerSpec{
			Kind: "mpls",
			MPLS: &event.MPLSLayer{
				Label: event.MPLSLabel{
					Label: label >> 12,
					TC:    uint8((label >> 9) & 0x7),
					BOS:   ((label >> 8) & 0x1) == 1,
					TTL:   uint8(label & 0xff),
				},
			},
		})
		offset += 4
		if label&0x100 != 0 {
			break
		}
	}
	if len(data) <= offset {
		return 0, 0, fmt.Errorf("missing mpls payload")
	}
	switch data[offset] >> 4 {
	case 4:
		return offset, 0x0800, nil
	case 6:
		return offset, 0x86dd, nil
	default:
		return 0, 0, fmt.Errorf("unsupported mpls payload")
	}
}

func parsePPPoE(data []byte, view *packetView) (int, uint16, error) {
	if len(data) < 8 {
		return 0, 0, fmt.Errorf("truncated pppoe session header")
	}
	proto := binary.BigEndian.Uint16(data[6:8])
	view.appendLayer(event.LayerSpec{
		Kind: "pppoe",
		PPPoE: &event.PPPoELayer{
			SessionID: binary.BigEndian.Uint16(data[2:4]),
			Protocol:  proto,
		},
	})
	switch proto {
	case 0x0021:
		return 8, 0x0800, nil
	case 0x0057:
		return 8, 0x86dd, nil
	default:
		return 0, 0, fmt.Errorf("unsupported ppp payload")
	}
}

func appendTransportLayer(view *packetView, proto uint32) {
	switch proto {
	case 6:
		layer := event.LayerSpec{Kind: "tcp"}
		if len(view.Tuples) > 0 {
			tuple := view.Tuples[len(view.Tuples)-1]
			layer.TCP = &event.TCPLayer{
				SrcPort: uint16(tuple.SrcPort),
				DstPort: uint16(tuple.DstPort),
			}
		}
		view.appendLayer(layer)
	case 17:
		layer := event.LayerSpec{Kind: "udp"}
		if len(view.Tuples) > 0 {
			tuple := view.Tuples[len(view.Tuples)-1]
			layer.UDP = &event.UDPLayer{
				SrcPort: uint16(tuple.SrcPort),
				DstPort: uint16(tuple.DstPort),
			}
		}
		view.appendLayer(layer)
	case 132:
		view.appendLayer(event.LayerSpec{
			Kind: "sctp",
			Features: map[string]event.FeatureValue{
				"transport": event.FeatureString("sctp"),
			},
		})
	case 1:
		view.appendLayer(event.LayerSpec{
			Kind: "icmp",
			ICMP: &event.ICMPLayer{},
		})
	case 58:
		view.appendLayer(event.LayerSpec{
			Kind: "icmpv6",
			ICMP: &event.ICMPLayer{},
		})
	}
}

func formatMAC(data []byte) string {
	if len(data) != 6 {
		return ""
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", data[0], data[1], data[2], data[3], data[4], data[5])
}

func isIPv6ExtensionHeader(nextHeader byte) bool {
	switch nextHeader {
	case 0, 43, 44, 50, 51, 60, 135, 139, 140:
		return true
	default:
		return false
	}
}

func ensureFields(evt *event.Event, capacity int) map[string]any {
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, capacity)
	}
	return evt.Fields
}

func fieldStringOrZero(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	val, ok := fields[key]
	if !ok {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func fieldUint32(fields map[string]any, key string) uint32 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	return uint32FromAny(val)
}

func uint32FromAny(val any) uint32 {
	switch v := val.(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case int:
		return uint32(v)
	case int64:
		return uint32(v)
	case float64:
		return uint32(v)
	case string:
		var n uint64
		fmt.Sscan(v, &n)
		return uint32(n)
	default:
		return 0
	}
}

func ipProtocolName(proto uint32) string {
	switch proto {
	case 1:
		return "icmp"
	case 2:
		return "igmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 41:
		return "ipv6"
	case 47:
		return "gre"
	case 50:
		return "esp"
	case 51:
		return "ah"
	case 58:
		return "icmpv6"
	case 132:
		return "sctp"
	default:
		return ""
	}
}

func sampledHeaderProtocolName(proto uint32) string {
	switch proto {
	case 1:
		return "ethernet"
	case 11:
		return "ipv4"
	case 12:
		return "ipv6"
	default:
		return ""
	}
}
