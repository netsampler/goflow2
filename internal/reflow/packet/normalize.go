package packet

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type NormalizeOptions struct {
	DisablePacketMapping bool
	TruncatePacketBytes  int
	UsePayloadAsPacket   bool
	TruncatePayload      bool
	HeaderProtocol       uint32
	Decode               DecodeOptions
	Extractors           []FeatureExtractor
}

type DecodeOptions struct {
	Configured     bool
	DecodeBeyondL4 bool
	DecodeGRE      bool
	DecodeIPIP     bool
	DecodeVXLAN    bool
	VXLANPorts     []uint32
	DecodeGeneve   bool
	GenevePorts    []uint32
	DecodeL2TP     bool
	L2TPPorts      []uint32
	DecodeGTPU     bool
	GTPUPorts      []uint32
	DecodePPPoE    bool
}

var defaultDecodeOptions = DecodeOptions{
	Configured:     true,
	DecodeBeyondL4: true,
	DecodeGRE:      true,
	DecodeIPIP:     true,
	DecodeVXLAN:    true,
	DecodeGeneve:   true,
	DecodeL2TP:     true,
	DecodeGTPU:     true,
	DecodePPPoE:    true,
}

func DefaultDecodeOptions() DecodeOptions {
	opts := defaultDecodeOptions
	opts.VXLANPorts = []uint32{4789}
	opts.GenevePorts = []uint32{6081}
	opts.L2TPPorts = []uint32{1701}
	opts.GTPUPorts = []uint32{2152}
	return opts
}

func (opts DecodeOptions) withDefaults() DecodeOptions {
	if !opts.Configured {
		return defaultDecodeOptions
	}
	return opts
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
	setDefaultInterfaces(evt, fields)

	if view, err := parsePacketViewWithOptions(headerData, opts.HeaderProtocol, opts.Decode.withDefaults()); err == nil {
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

// BuildPseudoHeader synthesizes minimal packet bytes from a packet model or
// canonical tuple fields. It is intended for encoders that require packet bytes
// even when the input event only carries flow fields.
func BuildPseudoHeader(evt *event.Event, fields map[string]any) ([]byte, bool) {
	var model *event.PacketModel
	if evt != nil {
		model = evt.Packet
	}
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
	inner := pseudoIPLayerFromFields(fields, "src_addr", "dst_addr", "proto", "src_port", "dst_port")
	outer := pseudoIPLayer{}
	outer = pseudoIPLayerFromFields(fields, "outer_src_addr", "outer_dst_addr", "outer_proto", "outer_src_port", "outer_dst_port")
	if !inner.Valid() {
		return nil
	}

	model := &event.PacketModel{
		Features: make(map[string]event.FeatureValue),
	}

	if pseudoPacketHasLinkLayer(fields) {
		dstMAC := fieldStringOrZero(fields, "dst_mac")
		if dstMAC == "" {
			// Pseudo packets may need an L2 envelope without source metadata.
			// Use zero MACs so the frame remains structurally valid.
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
	}

	if outer.Valid() {
		appendPseudoIPLayer(model, outer.SrcAddr, outer.DstAddr, outer.Proto)
		appendPseudoTunnelLayers(model, outer, inner)
	}

	appendPseudoIPLayer(model, inner.SrcAddr, inner.DstAddr, inner.Proto)
	appendPseudoTransportLayer(model, inner.Proto, inner.SrcPort, inner.DstPort)

	if frameLen := fieldUint32(fields, "original_length"); frameLen != 0 {
		model.Features["target_wire_length"] = event.FeatureUint64(uint64(frameLen))
	}
	return model
}

func pseudoPacketHasLinkLayer(fields map[string]any) bool {
	if fieldStringOrZero(fields, "src_mac") != "" || fieldStringOrZero(fields, "dst_mac") != "" {
		return true
	}
	if fieldUint32(fields, "ether_type") != 0 ||
		fieldUint32(fields, "vlan_id") != 0 ||
		fieldUint32(fields, "mpls_label") != 0 ||
		fieldUint32(fields, "pppoe_session_id") != 0 {
		return true
	}
	if vals, ok := fields["vlan_ids"].([]uint32); ok && len(vals) > 0 {
		return true
	}
	return false
}

type pseudoIPLayer struct {
	SrcAddr netip.Addr
	DstAddr netip.Addr
	Proto   uint32
	SrcPort uint32
	DstPort uint32
}

func (l pseudoIPLayer) Valid() bool {
	return l.SrcAddr.IsValid() && l.DstAddr.IsValid()
}

func pseudoIPLayerFromFields(fields map[string]any, srcKey, dstKey, protoKey, srcPortKey, dstPortKey string) pseudoIPLayer {
	srcAddr, err := netip.ParseAddr(fieldStringOrZero(fields, srcKey))
	if err != nil {
		return pseudoIPLayer{}
	}
	dstAddr, err := netip.ParseAddr(fieldStringOrZero(fields, dstKey))
	if err != nil {
		return pseudoIPLayer{}
	}
	return pseudoIPLayer{
		SrcAddr: srcAddr,
		DstAddr: dstAddr,
		Proto:   fieldUint32(fields, protoKey),
		SrcPort: fieldUint32(fields, srcPortKey),
		DstPort: fieldUint32(fields, dstPortKey),
	}
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

func appendPseudoTunnelLayers(model *event.PacketModel, outer, inner pseudoIPLayer) {
	if model == nil {
		return
	}
	switch pseudoTunnelKind(outer) {
	case "gre":
		model.Layers = append(model.Layers, event.LayerSpec{
			Kind: "gre",
			GRE:  &event.GRELayer{Protocol: pseudoInnerEtherType(inner.SrcAddr, inner.DstAddr)},
		})
	case "vxlan":
		model.Layers = append(model.Layers,
			event.LayerSpec{Kind: "udp", UDP: &event.UDPLayer{SrcPort: uint16(outer.SrcPort), DstPort: uint16(outer.DstPort)}},
			event.LayerSpec{Kind: "vxlan", VXLAN: &event.VXLANLayer{}},
			event.LayerSpec{Kind: "ethernet", Ethernet: &event.EthernetLayer{SrcMAC: "00:00:00:00:00:00", DstMAC: "00:00:00:00:00:00"}},
		)
	case "geneve":
		model.Layers = append(model.Layers,
			event.LayerSpec{Kind: "udp", UDP: &event.UDPLayer{SrcPort: uint16(outer.SrcPort), DstPort: uint16(outer.DstPort)}},
			event.LayerSpec{Kind: "geneve", Geneve: &event.GeneveLayer{Protocol: pseudoInnerEtherType(inner.SrcAddr, inner.DstAddr)}},
			event.LayerSpec{Kind: "ethernet", Ethernet: &event.EthernetLayer{SrcMAC: "00:00:00:00:00:00", DstMAC: "00:00:00:00:00:00"}},
		)
	}
}

func pseudoTunnelKind(outer pseudoIPLayer) string {
	switch {
	case outer.Proto == 47:
		return "gre"
	case outer.Proto == 17 && (outer.SrcPort == 4789 || outer.DstPort == 4789):
		return "vxlan"
	case outer.Proto == 17 && (outer.SrcPort == 6081 || outer.DstPort == 6081):
		return "geneve"
	default:
		return ""
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
		var next event.LayerSpec
		if i+1 < len(model.Layers) {
			next = model.Layers[i+1]
		}
		var err error
		payload, err = prependLayer(layer, payload, next)
		if err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func prependLayer(layer event.LayerSpec, payload []byte, next event.LayerSpec) ([]byte, error) {
	switch layer.Kind {
	case "ethernet":
		return prependEthernet(layer.Ethernet, payload, next)
	case "dot1q":
		return prependVLAN(layer.VLAN, payload, next)
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

func prependEthernet(layer *event.EthernetLayer, payload []byte, next event.LayerSpec) ([]byte, error) {
	if layer == nil {
		layer = &event.EthernetLayer{}
	}
	out := make([]byte, 14+len(payload))
	copy(out[0:6], parseMACOrZero(layer.DstMAC))
	copy(out[6:12], parseMACOrZero(layer.SrcMAC))
	etherType := inferLayerEtherType(layer.EtherType, next, payload)
	binary.BigEndian.PutUint16(out[12:14], uint16(etherType))
	copy(out[14:], payload)
	return out, nil
}

func prependVLAN(layer *event.VLANLayer, payload []byte, next event.LayerSpec) ([]byte, error) {
	if layer == nil {
		layer = &event.VLANLayer{TPID: 0x8100}
	}
	out := make([]byte, 4+len(payload))
	tci := uint16(layer.ID&0x0fff) | uint16(layer.PCP&0x7)<<13
	if layer.DEI {
		tci |= 1 << 12
	}
	binary.BigEndian.PutUint16(out[0:2], tci)
	binary.BigEndian.PutUint16(out[2:4], uint16(inferLayerEtherType(0, next, payload)))
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

func inferLayerEtherType(explicit uint32, next event.LayerSpec, payload []byte) uint32 {
	if explicit != 0 {
		return explicit
	}
	switch next.Kind {
	case "dot1q":
		if next.VLAN != nil && next.VLAN.TPID != 0 {
			return uint32(next.VLAN.TPID)
		}
		return 0x8100
	case "mpls":
		return 0x8847
	case "pppoe":
		return 0x8864
	case "ipv4":
		return 0x0800
	case "ipv6":
		return 0x86dd
	case "arp":
		return 0x0806
	default:
		return inferEtherType(0, payload)
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
	Layers    []string
	SrcMAC    string
	DstMAC    string
	EtherType uint32
	VLANIDs   []uint32
	Tuples    []packetTuple
	Model     *event.PacketModel
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
}

func packetTupleLayers(tuples []packetTuple) []map[string]any {
	layers := make([]map[string]any, 0, len(tuples))
	for i, tuple := range tuples {
		role := "single"
		if len(tuples) > 1 {
			role = "inner"
			if i == 0 {
				role = "outer"
			}
		}
		layers = append(layers, map[string]any{
			"index":      uint32(i),
			"role":       role,
			"src_addr":   tuple.SrcAddr.String(),
			"dst_addr":   tuple.DstAddr.String(),
			"proto":      tuple.Proto,
			"proto_name": ipProtocolName(tuple.Proto),
			"src_port":   tuple.SrcPort,
			"dst_port":   tuple.DstPort,
		})
	}
	return layers
}

// ApplyModelFields projects a packet model into the canonical tuple aliases
// used by aggregation and encoders. The packet model remains the authoritative
// full layer structure.
func ApplyModelFields(fields map[string]any, model *event.PacketModel) {
	if fields == nil || model == nil {
		return
	}
	applyPacketViewFields(fields, packetViewFromModel(model))
}

func packetViewFromModel(model *event.PacketModel) packetView {
	view := packetView{
		Layers: make([]string, 0, len(model.Layers)),
		Tuples: make([]packetTuple, 0, packetIPDepth(model)),
		Model:  model,
	}
	currentTuple := -1
	for _, layer := range model.Layers {
		if layer.Kind != "" {
			view.Layers = append(view.Layers, layer.Kind)
		}
		switch layer.Kind {
		case "ethernet":
			if layer.Ethernet == nil {
				continue
			}
			view.SrcMAC = layer.Ethernet.SrcMAC
			view.DstMAC = layer.Ethernet.DstMAC
			view.EtherType = layer.Ethernet.EtherType
		case "dot1q":
			if layer.VLAN != nil {
				view.VLANIDs = append(view.VLANIDs, uint32(layer.VLAN.ID))
			}
		case "ipv4":
			if layer.IPv4 == nil {
				continue
			}
			view.Tuples = append(view.Tuples, packetTuple{
				SrcAddr: layer.IPv4.SrcAddr,
				DstAddr: layer.IPv4.DstAddr,
				Proto:   uint32(layer.IPv4.Protocol),
			})
			currentTuple = len(view.Tuples) - 1
		case "ipv6":
			if layer.IPv6 == nil {
				continue
			}
			view.Tuples = append(view.Tuples, packetTuple{
				SrcAddr: layer.IPv6.SrcAddr,
				DstAddr: layer.IPv6.DstAddr,
				Proto:   uint32(layer.IPv6.NextHeader),
			})
			currentTuple = len(view.Tuples) - 1
		case "tcp":
			if layer.TCP == nil || currentTuple < 0 {
				continue
			}
			if view.Tuples[currentTuple].Proto == 0 {
				view.Tuples[currentTuple].Proto = 6
			}
			view.Tuples[currentTuple].SrcPort = uint32(layer.TCP.SrcPort)
			view.Tuples[currentTuple].DstPort = uint32(layer.TCP.DstPort)
		case "udp":
			if layer.UDP == nil || currentTuple < 0 {
				continue
			}
			if view.Tuples[currentTuple].Proto == 0 {
				view.Tuples[currentTuple].Proto = 17
			}
			view.Tuples[currentTuple].SrcPort = uint32(layer.UDP.SrcPort)
			view.Tuples[currentTuple].DstPort = uint32(layer.UDP.DstPort)
		}
	}
	return view
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
	layerFeatures := make([]event.FeatureValue, len(view.Layers))
	for i, layer := range view.Layers {
		layerFeatures[i] = event.FeatureString(layer)
	}
	view.Model.Features["layer_kinds"] = event.FeatureValue{List: layerFeatures}
	view.Model.Features["ip_depth"] = event.FeatureUint64(uint64(packetIPDepth(view.Model)))
	if len(view.Tuples) > 1 {
		view.Model.Features["encap_depth"] = event.FeatureUint64(uint64(len(view.Tuples) - 1))
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
	return parsePacketViewWithOptions(data, 0, defaultDecodeOptions)
}

func parsePacketViewWithProtocol(data []byte, protocol uint32) (packetView, error) {
	return parsePacketViewWithOptions(data, protocol, defaultDecodeOptions)
}

func parsePacketViewWithOptions(data []byte, protocol uint32, opts DecodeOptions) (packetView, error) {
	if len(data) == 0 {
		return packetView{}, fmt.Errorf("empty packet header")
	}
	view := packetView{
		Layers: make([]string, 0, 6),
		Tuples: make([]packetTuple, 0, 2),
		Model: &event.PacketModel{
			Layers:   make([]event.LayerSpec, 0, 8),
			Features: make(map[string]event.FeatureValue, 2),
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
			if innerEtherType, ok := ipInIPInnerEtherType(nextProto, opts); ok {
				offset += nextOffset
				etherType = innerEtherType
				continue
			}
			if opts.DecodeGRE && nextProto == 47 {
				innerOffset, innerProto, err := parseGRE(data[offset+nextOffset:], &view)
				if err != nil {
					return view, nil
				}
				if !opts.DecodeBeyondL4 {
					return finalizePacketView(view), nil
				}
				offset += nextOffset + innerOffset
				etherType = innerProto
				if shouldParseEthernetPayload(etherType) {
					nextOffset, nextEtherType, err := parseEthernet(data[offset:], &view, false)
					if err != nil {
						return view, nil
					}
					offset += nextOffset
					etherType = nextEtherType
					continue
				}
				if canContinueEtherType(etherType) {
					continue
				}
			}
			if opts.DecodeBeyondL4 && tuple.Proto == 17 {
				if innerOffset, innerEtherType, err := parseUDPTunnel(data[offset+nextOffset:], tuple, &view, opts); err == nil {
					offset += nextOffset + innerOffset
					if shouldParseEthernetPayload(innerEtherType) {
						nextOffset, nextEtherType, err := parseEthernet(data[offset:], &view, false)
						if err != nil {
							return view, nil
						}
						offset += nextOffset
						etherType = nextEtherType
						continue
					}
					etherType = innerEtherType
					if canContinueEtherType(etherType) {
						continue
					}
				}
			}
			appendTransportLayer(&view, tuple.Proto)
			return finalizePacketView(view), nil
		case etherType == 0x86dd || data[offset]>>4 == 6:
			nextOffset, tuple, nextProto, extensionLayers, err := parseIPv6Tuple(data[offset:])
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
			for _, layer := range extensionLayers {
				view.appendLayer(layer)
			}
			view.Tuples = append(view.Tuples, tuple)
			if innerEtherType, ok := ipInIPInnerEtherType(nextProto, opts); ok {
				offset += nextOffset
				etherType = innerEtherType
				continue
			}
			if opts.DecodeGRE && nextProto == 47 {
				innerOffset, innerProto, err := parseGRE(data[offset+nextOffset:], &view)
				if err != nil {
					return view, nil
				}
				if !opts.DecodeBeyondL4 {
					return finalizePacketView(view), nil
				}
				offset += nextOffset + innerOffset
				etherType = innerProto
				if shouldParseEthernetPayload(etherType) {
					nextOffset, nextEtherType, err := parseEthernet(data[offset:], &view, false)
					if err != nil {
						return view, nil
					}
					offset += nextOffset
					etherType = nextEtherType
					continue
				}
				if canContinueEtherType(etherType) {
					continue
				}
			}
			if opts.DecodeBeyondL4 && tuple.Proto == 17 {
				if innerOffset, innerEtherType, err := parseUDPTunnel(data[offset+nextOffset:], tuple, &view, opts); err == nil {
					offset += nextOffset + innerOffset
					if shouldParseEthernetPayload(innerEtherType) {
						nextOffset, nextEtherType, err := parseEthernet(data[offset:], &view, false)
						if err != nil {
							return view, nil
						}
						offset += nextOffset
						etherType = nextEtherType
						continue
					}
					etherType = innerEtherType
					if canContinueEtherType(etherType) {
						continue
					}
				}
			}
			appendTransportLayer(&view, tuple.Proto)
			return finalizePacketView(view), nil
		case etherType == 0x8847 || etherType == 0x8848:
			innerOffset, innerProto, err := parseMPLS(data[offset:], &view)
			if err != nil {
				return view, nil
			}
			if !opts.DecodeBeyondL4 {
				return finalizePacketView(view), nil
			}
			offset += innerOffset
			etherType = innerProto
			continue
		case etherType == 0x8864:
			if !opts.DecodePPPoE {
				return finalizePacketView(view), nil
			}
			innerOffset, innerProto, err := parsePPPoE(data[offset:], &view)
			if err != nil {
				return view, nil
			}
			if !opts.DecodeBeyondL4 {
				return finalizePacketView(view), nil
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

func shouldParseEthernetPayload(etherType uint16) bool {
	return etherType == 0x6558
}

func canContinueEtherType(etherType uint16) bool {
	switch etherType {
	case 0x0800, 0x86dd, 0x8847, 0x8848, 0x8864:
		return true
	default:
		return false
	}
}

func ipInIPInnerEtherType(protocol uint32, opts DecodeOptions) (uint16, bool) {
	if !opts.DecodeBeyondL4 || !opts.DecodeIPIP {
		return 0, false
	}
	switch protocol {
	case 4:
		return 0x0800, true
	case 41:
		return 0x86dd, true
	default:
		return 0, false
	}
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

func parseIPv6Tuple(data []byte) (int, packetTuple, uint32, []event.LayerSpec, error) {
	if len(data) < 40 {
		return 0, packetTuple{}, 0, nil, fmt.Errorf("truncated ipv6 header")
	}
	src, ok := netip.AddrFromSlice(data[8:24])
	if !ok {
		return 0, packetTuple{}, 0, nil, fmt.Errorf("invalid ipv6 source address")
	}
	dst, ok := netip.AddrFromSlice(data[24:40])
	if !ok {
		return 0, packetTuple{}, 0, nil, fmt.Errorf("invalid ipv6 destination address")
	}
	nextHeader := data[6]
	offset := 40
	var extensionLayers []event.LayerSpec
	for {
		if !isIPv6ExtensionHeader(nextHeader) {
			break
		}
		if len(data) < offset+2 {
			return 0, packetTuple{}, 0, nil, fmt.Errorf("truncated ipv6 extension header")
		}
		switch nextHeader {
		case 44:
			if len(data) < offset+8 {
				return 0, packetTuple{}, 0, nil, fmt.Errorf("truncated ipv6 fragment header")
			}
			extensionLayers = append(extensionLayers, event.LayerSpec{
				Kind: "ipv6_fragment",
				Features: map[string]event.FeatureValue{
					"next_header": event.FeatureUint64(uint64(data[offset])),
				},
			})
			nextHeader = data[offset]
			offset += 8
		case 51:
			hdrLen := (int(data[offset+1]) + 2) * 4
			if len(data) < offset+hdrLen {
				return 0, packetTuple{}, 0, nil, fmt.Errorf("truncated ipv6 authentication header")
			}
			extensionLayers = append(extensionLayers, event.LayerSpec{
				Kind: "ipv6_authentication",
				Features: map[string]event.FeatureValue{
					"next_header": event.FeatureUint64(uint64(data[offset])),
				},
			})
			nextHeader = data[offset]
			offset += hdrLen
		default:
			hdrLen := (int(data[offset+1]) + 1) * 8
			if len(data) < offset+hdrLen {
				return 0, packetTuple{}, 0, nil, fmt.Errorf("truncated ipv6 extension header")
			}
			if nextHeader == 43 {
				extensionLayers = append(extensionLayers, event.LayerSpec{
					Kind: "ipv6_routing",
					Features: map[string]event.FeatureValue{
						"next_header":  event.FeatureUint64(uint64(data[offset])),
						"routing_type": event.FeatureUint64(uint64(data[offset+2])),
					},
				})
			} else {
				extensionLayers = append(extensionLayers, event.LayerSpec{
					Kind: "ipv6_extension",
					Features: map[string]event.FeatureValue{
						"header":      event.FeatureUint64(uint64(nextHeader)),
						"next_header": event.FeatureUint64(uint64(data[offset])),
					},
				})
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
	return offset, tuple, tuple.Proto, extensionLayers, nil
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

func parseUDPTunnel(data []byte, tuple packetTuple, view *packetView, opts DecodeOptions) (int, uint16, error) {
	if len(data) < 8 {
		return 0, 0, fmt.Errorf("truncated udp header")
	}
	switch {
	case opts.DecodeVXLAN && portMatches(tuple, opts.VXLANPorts, 4789):
		if len(data) < 16 {
			return 0, 0, fmt.Errorf("truncated vxlan header")
		}
		view.appendLayer(event.LayerSpec{
			Kind: "vxlan",
			VXLAN: &event.VXLANLayer{
				VNI: uint32(data[12])<<16 | uint32(data[13])<<8 | uint32(data[14]),
			},
		})
		return 16, 0x6558, nil
	case opts.DecodeGeneve && portMatches(tuple, opts.GenevePorts, 6081):
		if len(data) < 16 {
			return 0, 0, fmt.Errorf("truncated geneve header")
		}
		optLen := int((data[8] & 0x3f) * 4)
		proto := binary.BigEndian.Uint16(data[10:12])
		offset := 8 + 8 + optLen
		if len(data) < offset {
			return 0, 0, fmt.Errorf("truncated geneve options")
		}
		view.appendLayer(event.LayerSpec{
			Kind: "geneve",
			Geneve: &event.GeneveLayer{
				VNI:      uint32(data[12])<<16 | uint32(data[13])<<8 | uint32(data[14]),
				Protocol: proto,
			},
		})
		return offset, proto, nil
	case opts.DecodeL2TP && portMatches(tuple, opts.L2TPPorts, 1701):
		return parseL2TP(data, view)
	case opts.DecodeGTPU && portMatches(tuple, opts.GTPUPorts, 2152):
		return parseGTPU(data, view)
	default:
		return 0, 0, fmt.Errorf("not a supported udp tunnel")
	}
}

func parseL2TP(data []byte, view *packetView) (int, uint16, error) {
	if len(data) < 14 {
		return 0, 0, fmt.Errorf("truncated l2tp header")
	}
	flags := binary.BigEndian.Uint16(data[8:10])
	version := flags & 0x000f
	if version != 2 {
		return 0, 0, fmt.Errorf("unsupported l2tp version")
	}
	offset := 10
	if flags&0x4000 != 0 {
		if len(data) < offset+2 {
			return 0, 0, fmt.Errorf("truncated l2tp length")
		}
		offset += 2
	}
	if len(data) < offset+4 {
		return 0, 0, fmt.Errorf("truncated l2tp session")
	}
	tunnelID := binary.BigEndian.Uint16(data[offset : offset+2])
	sessionID := binary.BigEndian.Uint16(data[offset+2 : offset+4])
	offset += 4
	if flags&0x0800 != 0 {
		if len(data) < offset+4 {
			return 0, 0, fmt.Errorf("truncated l2tp sequence")
		}
		offset += 4
	}
	if flags&0x0200 != 0 {
		if len(data) < offset+2 {
			return 0, 0, fmt.Errorf("truncated l2tp offset size")
		}
		offsetSize := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2 + offsetSize
	}
	if len(data) < offset+2 {
		return 0, 0, fmt.Errorf("truncated l2tp payload")
	}
	if len(data) >= offset+4 && data[offset] == 0xff && data[offset+1] == 0x03 {
		offset += 2
	}
	proto := binary.BigEndian.Uint16(data[offset : offset+2])
	offset += 2
	view.appendLayer(event.LayerSpec{
		Kind: "l2tp",
		Features: map[string]event.FeatureValue{
			"tunnel_id":  event.FeatureUint64(uint64(tunnelID)),
			"session_id": event.FeatureUint64(uint64(sessionID)),
			"version":    event.FeatureUint64(uint64(version)),
		},
	})
	switch proto {
	case 0x0021:
		return offset, 0x0800, nil
	case 0x0057:
		return offset, 0x86dd, nil
	default:
		return 0, 0, fmt.Errorf("unsupported l2tp ppp payload")
	}
}

func parseGTPU(data []byte, view *packetView) (int, uint16, error) {
	if len(data) < 16 {
		return 0, 0, fmt.Errorf("truncated gtpu header")
	}
	flags := data[8]
	messageType := data[9]
	if flags>>5 != 1 || messageType != 0xff {
		return 0, 0, fmt.Errorf("unsupported gtpu message")
	}
	teid := binary.BigEndian.Uint32(data[12:16])
	offset := 16
	if flags&0x07 != 0 {
		if len(data) < offset+4 {
			return 0, 0, fmt.Errorf("truncated gtpu optional fields")
		}
		nextExt := data[offset+3]
		offset += 4
		for nextExt != 0 {
			if len(data) < offset+2 {
				return 0, 0, fmt.Errorf("truncated gtpu extension")
			}
			extLen := int(data[offset+1]) * 4
			if extLen == 0 || len(data) < offset+2+extLen {
				return 0, 0, fmt.Errorf("truncated gtpu extension payload")
			}
			nextExt = data[offset+1+extLen]
			offset += 2 + extLen
		}
	}
	if len(data) <= offset {
		return 0, 0, fmt.Errorf("missing gtpu payload")
	}
	view.appendLayer(event.LayerSpec{
		Kind: "gtpu",
		Features: map[string]event.FeatureValue{
			"teid":         event.FeatureUint64(uint64(teid)),
			"message_type": event.FeatureUint64(uint64(messageType)),
		},
	})
	switch data[offset] >> 4 {
	case 4:
		return offset, 0x0800, nil
	case 6:
		return offset, 0x86dd, nil
	default:
		return 0, 0, fmt.Errorf("unsupported gtpu payload")
	}
}

func portMatches(tuple packetTuple, ports []uint32, defaultPort uint32) bool {
	if len(ports) == 0 {
		return tuple.DstPort == defaultPort || tuple.SrcPort == defaultPort
	}
	for _, port := range ports {
		if port != 0 && (tuple.DstPort == port || tuple.SrcPort == port) {
			return true
		}
	}
	return false
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
	case 0x0281, 0x0283:
		return 8, 0x8847, nil
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
	const hex = "0123456789abcdef"
	var out [17]byte
	j := 0
	for i := 0; i < 6; i++ {
		if i > 0 {
			out[j] = ':'
			j++
		}
		out[j] = hex[data[i]>>4]
		out[j+1] = hex[data[i]&0x0f]
		j += 2
	}
	return string(out[:])
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

func stringFromAny(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case nil:
		return ""
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
	case json.Number:
		n, err := v.Int64()
		if err != nil || n < 0 {
			return 0
		}
		return uint32(n)
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
