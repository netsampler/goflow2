package processor

import (
	"encoding/binary"
	"encoding/json"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func TestBuiltinProcessFlowDropsPayload(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			DropMessage: true,
			DropPayload: true,
		},
	})

	evt := &event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Message: []byte(`{"ignored":true}`),
		Payload: []byte{1, 2, 3},
		Fields: map[string]any{
			"flow_type": "sflow",
		},
	}

	events, err := proc.Process(evt)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Message != nil {
		t.Fatalf("expected message to be dropped")
	}
	if events[0].Payload != nil {
		t.Fatalf("expected payload to be dropped")
	}
}

func TestBuiltinProcessFlowRequiresFlowType(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{},
	})

	_, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{Type: "flow"},
	})
	if err == nil {
		t.Fatalf("expected error for missing flow_type")
	}
}

func TestBuiltinProcessFlowNormalizesPacketRecordWithoutTruncatingDatagramPayload(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			TruncatePacketBytes: 32,
		},
	})

	header := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	datagram := make([]byte, 128)

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Payload: datagram,
		Fields: map[string]any{
			"flow_type":       "sflow",
			"record_kind":     "packet",
			"protocol":        uint32(1),
			"original_length": uint32(len(header)),
			"frame_length":    uint32(len(header)),
			"header_data":     append([]byte(nil), header...),
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	fields := events[0].Fields
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected src_addr=192.0.2.1, got %#v", got)
	}
	if got := fields["dst_addr"]; got != "198.51.100.2" {
		t.Fatalf("expected dst_addr=198.51.100.2, got %#v", got)
	}
	if got := fields["src_port"]; got != uint32(12345) {
		t.Fatalf("expected src_port=12345, got %#v", got)
	}
	if got := fields["dst_port"]; got != uint32(443) {
		t.Fatalf("expected dst_port=443, got %#v", got)
	}
	headerData, ok := fields["header_data"].([]byte)
	if !ok {
		t.Fatalf("expected header_data to be []byte, got %T", fields["header_data"])
	}
	if len(headerData) != 32 {
		t.Fatalf("expected truncated header_data length=32, got %d", len(headerData))
	}
	payload, ok := events[0].Payload.([]byte)
	if !ok {
		t.Fatalf("expected payload to remain []byte, got %T", events[0].Payload)
	}
	if len(payload) != len(datagram) {
		t.Fatalf("expected exporter datagram payload length=%d, got %d", len(datagram), len(payload))
	}
}

func TestBuiltinProcessBytesDecodesPacketTuple(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			DropPayload: false,
		},
	})

	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	evt := &event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: packet,
	}

	events, err := proc.Process(evt)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	fields := events[0].Fields
	if got := fields["record_kind"]; got != "packet" {
		t.Fatalf("expected record_kind=packet, got %#v", got)
	}
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected src_addr=192.0.2.1, got %#v", got)
	}
	if got := fields["dst_addr"]; got != "198.51.100.2" {
		t.Fatalf("expected dst_addr=198.51.100.2, got %#v", got)
	}
	if got := fields["src_port"]; got != uint32(12345) {
		t.Fatalf("expected src_port=12345, got %#v", got)
	}
	if got := fields["dst_port"]; got != uint32(443) {
		t.Fatalf("expected dst_port=443, got %#v", got)
	}
	if got := fields["proto"]; got != uint32(6) {
		t.Fatalf("expected proto=6, got %#v", got)
	}
	if got := fields["bytes"]; got != int64(len(packet)) {
		t.Fatalf("expected bytes=%d, got %#v", len(packet), got)
	}
	headerData, ok := fields["header_data"].([]byte)
	if !ok {
		t.Fatalf("expected header_data to be []byte, got %T", fields["header_data"])
	}
	if len(headerData) != len(packet) {
		t.Fatalf("expected header_data length=%d, got %d", len(packet), len(headerData))
	}
	if events[0].Packet == nil {
		t.Fatalf("expected packet model to be attached")
	}
	if len(events[0].Packet.Layers) != 3 {
		t.Fatalf("expected 3 packet layers, got %d", len(events[0].Packet.Layers))
	}
}

func TestBuiltinProcessBytesSetsNanosecondPacketWindowFromInterfaceSpeed(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})
	receivedAt := time.Unix(1_700_000_000, 123_000_000).UTC()
	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	events, err := proc.Process(&event.Event{
		ReceivedAt: receivedAt,
		Source:     event.SourceMetadata{Type: "bytes"},
		Payload:    packet,
		Fields: map[string]any{
			"wire_length": uint32(250_000),
			"if_speed":    uint64(1_000_000_000),
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	fields := events[0].Fields
	startNS := receivedAt.UnixNano()
	endNS := startNS + int64(2*time.Millisecond)
	if got := fields["time_flow_start_ns"]; got != startNS {
		t.Fatalf("expected time_flow_start_ns=%d, got %#v", startNS, got)
	}
	if got := fields["time_flow_end_ns"]; got != endNS {
		t.Fatalf("expected time_flow_end_ns=%d, got %#v", endNS, got)
	}
	if got := fields["start_time_unix"]; got != receivedAt.UnixMilli() {
		t.Fatalf("expected start_time_unix=%d, got %#v", receivedAt.UnixMilli(), got)
	}
	if got := fields["end_time_unix"]; got != receivedAt.Add(2*time.Millisecond).UnixMilli() {
		t.Fatalf("expected end_time_unix=%d, got %#v", receivedAt.Add(2*time.Millisecond).UnixMilli(), got)
	}
}

func TestBuiltinProcessGoFlow2V2PreservesNanosecondTimeAliases(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})

	events, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{
			Type: "json",
			JSON: event.JSONMetadata{Flavor: "goflow2v2"},
		},
		Message: []byte(`{
			"type": 1,
			"time_flow_start_ns": 1700000000100123456,
			"time_flow_end_ns": 1700000000900123456
		}`),
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	fields := events[0].Fields
	if got := fields["time_flow_start_ns"]; got != int64(1_700_000_000_100_123_456) {
		t.Fatalf("expected time_flow_start_ns to preserve nanoseconds, got %#v", got)
	}
	if got := fields["time_flow_end_ns"]; got != int64(1_700_000_000_900_123_456) {
		t.Fatalf("expected time_flow_end_ns to preserve nanoseconds, got %#v", got)
	}
	if got := fields["start_time_unix"]; got != int64(1_700_000_000_100) {
		t.Fatalf("expected start_time_unix milliseconds, got %#v", got)
	}
	if got := fields["end_time_unix"]; got != int64(1_700_000_000_900) {
		t.Fatalf("expected end_time_unix milliseconds, got %#v", got)
	}
}

func TestBuiltinProcessBytesCanDisablePacketMapping(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			DisablePacketMapping: true,
		},
	})

	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: packet,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	if _, ok := fields["src_addr"]; ok {
		t.Fatalf("expected src_addr to be absent when packet mapping is disabled")
	}
	if _, ok := fields["dst_addr"]; ok {
		t.Fatalf("expected dst_addr to be absent when packet mapping is disabled")
	}
}

func TestBuiltinProcessBytesTruncatesRetainedPacketData(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			TruncatePacketBytes: 32,
		},
	})

	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: packet,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	headerData, ok := fields["header_data"].([]byte)
	if !ok {
		t.Fatalf("expected header_data to be []byte, got %T", fields["header_data"])
	}
	if len(headerData) != 32 {
		t.Fatalf("expected truncated header_data length=32, got %d", len(headerData))
	}
	payload, ok := events[0].Payload.([]byte)
	if !ok {
		t.Fatalf("expected payload to remain []byte, got %T", events[0].Payload)
	}
	if len(payload) != 32 {
		t.Fatalf("expected truncated payload length=32, got %d", len(payload))
	}
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected src_addr to be mapped before truncation, got %#v", got)
	}
}

func TestBuiltinProcessBytesFallsBackToCaptureInterfaceIndex(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})

	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	events, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{
			Type:                  "bytes",
			CaptureInterfaceIndex: 15,
		},
		Payload: packet,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	if got := fields["input_if"]; got != uint32(15) {
		t.Fatalf("expected input_if=15, got %#v", got)
	}
	if got := fields["output_if"]; got != uint32(15) {
		t.Fatalf("expected output_if=15, got %#v", got)
	}
}

func TestBuiltinProcessBytesExtractsEncapsulatedLayers(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})

	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x81, 0x00,
		0x00, 0x64,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x34, 0x00, 0x01, 0x00, 0x00, 0x40, 0x2f, 0x00, 0x00,
		0xcb, 0x00, 0x71, 0x01,
		0xcb, 0x00, 0x71, 0x02,
		0x00, 0x00, 0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x02, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: packet,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields

	if got := fields["src_mac"]; got != "66:77:88:99:aa:bb" {
		t.Fatalf("expected src_mac=66:77:88:99:aa:bb, got %#v", got)
	}
	if got := fields["dst_mac"]; got != "00:11:22:33:44:55" {
		t.Fatalf("expected dst_mac=00:11:22:33:44:55, got %#v", got)
	}
	if got := fields["vlan_id"]; got != uint32(100) {
		t.Fatalf("expected vlan_id=100, got %#v", got)
	}
	vlanIDs, ok := fields["vlan_ids"].([]uint32)
	if !ok {
		t.Fatalf("expected vlan_ids to be []uint32, got %T", fields["vlan_ids"])
	}
	if len(vlanIDs) != 1 || vlanIDs[0] != 100 {
		t.Fatalf("expected vlan_ids=[100], got %#v", vlanIDs)
	}
	layers, ok := fields["packet_layers"].([]string)
	if !ok {
		t.Fatalf("expected packet_layers to be []string, got %T", fields["packet_layers"])
	}
	expectedLayers := []string{"ethernet", "dot1q", "ipv4", "gre", "ipv4", "tcp"}
	if len(layers) != len(expectedLayers) {
		t.Fatalf("expected %d packet layers, got %#v", len(expectedLayers), layers)
	}
	for i, want := range expectedLayers {
		if layers[i] != want {
			t.Fatalf("expected packet_layers[%d]=%q, got %#v", i, want, layers[i])
		}
	}
	if got := fields["outer_src_addr"]; got != "203.0.113.1" {
		t.Fatalf("expected outer_src_addr=203.0.113.1, got %#v", got)
	}
	if got := fields["outer_dst_addr"]; got != "203.0.113.2" {
		t.Fatalf("expected outer_dst_addr=203.0.113.2, got %#v", got)
	}
	if got := fields["outer_proto"]; got != uint32(47) {
		t.Fatalf("expected outer_proto=47, got %#v", got)
	}
	expectIPLayer(t, fields, 0, "outer", "203.0.113.1", "203.0.113.2", 47, 0, 0)
	expectIPLayer(t, fields, 1, "inner", "192.0.2.1", "198.51.100.2", 6, 12345, 443)
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected inner src_addr=192.0.2.1, got %#v", got)
	}
	if got := fields["dst_addr"]; got != "198.51.100.2" {
		t.Fatalf("expected inner dst_addr=198.51.100.2, got %#v", got)
	}
	if got := fields["src_port"]; got != uint32(12345) {
		t.Fatalf("expected inner src_port=12345, got %#v", got)
	}
	if got := fields["dst_port"]; got != uint32(443) {
		t.Fatalf("expected inner dst_port=443, got %#v", got)
	}
}

func TestBuiltinProcessBytesExtractsVXLANInnerPacket(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})

	packet := vxlanTestPacket(4789)

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: packet,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields

	layers, ok := fields["packet_layers"].([]string)
	if !ok {
		t.Fatalf("expected packet_layers to be []string, got %T", fields["packet_layers"])
	}
	expectedLayers := []string{"ethernet", "ipv4", "vxlan", "ethernet", "ipv4", "tcp"}
	if len(layers) != len(expectedLayers) {
		t.Fatalf("expected %d packet layers, got %#v", len(expectedLayers), layers)
	}
	for i, want := range expectedLayers {
		if layers[i] != want {
			t.Fatalf("expected packet_layers[%d]=%q, got %#v", i, want, layers[i])
		}
	}
	if got := fields["outer_src_addr"]; got != "203.0.113.1" {
		t.Fatalf("expected outer_src_addr=203.0.113.1, got %#v", got)
	}
	if got := fields["outer_dst_addr"]; got != "203.0.113.2" {
		t.Fatalf("expected outer_dst_addr=203.0.113.2, got %#v", got)
	}
	expectIPLayer(t, fields, 0, "outer", "203.0.113.1", "203.0.113.2", 17, 4660, 4789)
	expectIPLayer(t, fields, 1, "inner", "192.0.2.1", "198.51.100.2", 6, 12345, 443)
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected inner src_addr=192.0.2.1, got %#v", got)
	}
	if got := fields["dst_addr"]; got != "198.51.100.2" {
		t.Fatalf("expected inner dst_addr=198.51.100.2, got %#v", got)
	}
}

func TestBuiltinProcessBytesCanDisableUDPTunnelDecoding(t *testing.T) {
	disabled := false
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			PacketDecoder: config.PacketDecoderConfig{
				DecodeBeyondL4: &disabled,
			},
		},
	})

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: vxlanTestPacket(4789),
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	layers, ok := fields["packet_layers"].([]string)
	if !ok {
		t.Fatalf("expected packet_layers to be []string, got %T", fields["packet_layers"])
	}
	expectedLayers := []string{"ethernet", "ipv4", "udp"}
	if len(layers) != len(expectedLayers) {
		t.Fatalf("expected %d packet layers, got %#v", len(expectedLayers), layers)
	}
	for i, want := range expectedLayers {
		if layers[i] != want {
			t.Fatalf("expected packet_layers[%d]=%q, got %#v", i, want, layers[i])
		}
	}
	if got := fields["src_addr"]; got != "203.0.113.1" {
		t.Fatalf("expected outer src_addr=203.0.113.1, got %#v", got)
	}
	if _, ok := fields["outer_src_addr"]; ok {
		t.Fatalf("did not expect outer_src_addr when UDP tunnel decoding is disabled")
	}
}

func TestBuiltinProcessBytesCanDisableVXLANEncapsulation(t *testing.T) {
	disabled := false
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			PacketDecoder: config.PacketDecoderConfig{
				Encapsulations: config.PacketEncapsulationConfig{
					VXLAN: config.PortEncapsulationConfig{
						Enabled: &disabled,
					},
				},
			},
		},
	})

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: vxlanTestPacket(4789),
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	layers, ok := fields["packet_layers"].([]string)
	if !ok {
		t.Fatalf("expected packet_layers to be []string, got %T", fields["packet_layers"])
	}
	expectedLayers := []string{"ethernet", "ipv4", "udp"}
	if len(layers) != len(expectedLayers) {
		t.Fatalf("expected %d packet layers, got %#v", len(expectedLayers), layers)
	}
	for i, want := range expectedLayers {
		if layers[i] != want {
			t.Fatalf("expected packet_layers[%d]=%q, got %#v", i, want, layers[i])
		}
	}
}

func TestBuiltinProcessBytesDecodesCustomVXLANPort(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			PacketDecoder: config.PacketDecoderConfig{
				Encapsulations: config.PacketEncapsulationConfig{
					VXLAN: config.PortEncapsulationConfig{
						Ports: []uint32{4790},
					},
				},
			},
		},
	})

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: vxlanTestPacket(4790),
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	expectIPLayer(t, fields, 0, "outer", "203.0.113.1", "203.0.113.2", 17, 4660, 4790)
	expectIPLayer(t, fields, 1, "inner", "192.0.2.1", "198.51.100.2", 6, 12345, 443)
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected inner src_addr=192.0.2.1, got %#v", got)
	}
}

func TestBuiltinProcessBytesExtractsIPInIP(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})
	inner := ipv4Packet(6, [4]byte{192, 0, 2, 1}, [4]byte{198, 51, 100, 2}, tcpHeader(12345, 443))
	outer := ipv4Packet(4, [4]byte{203, 0, 113, 1}, [4]byte{203, 0, 113, 2}, inner)

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: ethernetPayload(0x0800, outer),
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	expectIPLayer(t, fields, 0, "outer", "203.0.113.1", "203.0.113.2", 4, 0, 0)
	expectIPLayer(t, fields, 1, "inner", "192.0.2.1", "198.51.100.2", 6, 12345, 443)
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected inner src_addr=192.0.2.1, got %#v", got)
	}
}

func TestBuiltinProcessBytesExtractsIPv6InIPWithRoutingHeader(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})
	inner := ipv6RoutingPacket(tcpHeader(12345, 443))
	outer := ipv4Packet(41, [4]byte{203, 0, 113, 1}, [4]byte{203, 0, 113, 2}, inner)

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: ethernetPayload(0x0800, outer),
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	layers, ok := fields["packet_layers"].([]string)
	if !ok {
		t.Fatalf("expected packet_layers to be []string, got %T", fields["packet_layers"])
	}
	expectedLayers := []string{"ethernet", "ipv4", "ipv6", "ipv6_routing", "tcp"}
	if len(layers) != len(expectedLayers) {
		t.Fatalf("expected %d packet layers, got %#v", len(expectedLayers), layers)
	}
	for i, want := range expectedLayers {
		if layers[i] != want {
			t.Fatalf("expected packet_layers[%d]=%q, got %#v", i, want, layers[i])
		}
	}
	expectIPLayer(t, fields, 0, "outer", "203.0.113.1", "203.0.113.2", 41, 0, 0)
	expectIPLayer(t, fields, 1, "inner", "2001:db8::1", "2001:db8::2", 6, 12345, 443)
}

func TestBuiltinProcessBytesExtractsL2TPAndGTPUInnerPackets(t *testing.T) {
	tests := []struct {
		name       string
		payload    []byte
		wantLayer  string
		wantOuter  packetLayerExpectation
		wantInner  packetLayerExpectation
		wantLayers []string
	}{
		{
			name:      "l2tp",
			payload:   ethernetPayload(0x0800, ipv4Packet(17, [4]byte{203, 0, 113, 1}, [4]byte{203, 0, 113, 2}, udpPacket(49152, 1701, l2tpPayload(ipv4Packet(6, [4]byte{192, 0, 2, 1}, [4]byte{198, 51, 100, 2}, tcpHeader(12345, 443)))))),
			wantLayer: "l2tp",
			wantOuter: packetLayerExpectation{"outer", "203.0.113.1", "203.0.113.2", 17, 49152, 1701},
			wantInner: packetLayerExpectation{"inner", "192.0.2.1", "198.51.100.2", 6, 12345, 443},
		},
		{
			name:      "gtpu",
			payload:   ethernetPayload(0x0800, ipv4Packet(17, [4]byte{203, 0, 113, 1}, [4]byte{203, 0, 113, 2}, udpPacket(49152, 2152, gtpuPayload(ipv4Packet(6, [4]byte{192, 0, 2, 1}, [4]byte{198, 51, 100, 2}, tcpHeader(12345, 443)))))),
			wantLayer: "gtpu",
			wantOuter: packetLayerExpectation{"outer", "203.0.113.1", "203.0.113.2", 17, 49152, 2152},
			wantInner: packetLayerExpectation{"inner", "192.0.2.1", "198.51.100.2", 6, 12345, 443},
		},
	}

	proc := NewBuiltin(config.ProcessorConfig{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := proc.Process(&event.Event{
				Source:  event.SourceMetadata{Type: "bytes"},
				Payload: tt.payload,
			})
			if err != nil {
				t.Fatalf("Process returned error: %v", err)
			}
			fields := events[0].Fields
			layers, ok := fields["packet_layers"].([]string)
			if !ok {
				t.Fatalf("expected packet_layers to be []string, got %T", fields["packet_layers"])
			}
			if !containsLayer(layers, tt.wantLayer) {
				t.Fatalf("expected packet_layers to contain %q, got %#v", tt.wantLayer, layers)
			}
			expectIPLayer(t, fields, 0, tt.wantOuter.role, tt.wantOuter.srcAddr, tt.wantOuter.dstAddr, tt.wantOuter.proto, tt.wantOuter.srcPort, tt.wantOuter.dstPort)
			expectIPLayer(t, fields, 1, tt.wantInner.role, tt.wantInner.srcAddr, tt.wantInner.dstAddr, tt.wantInner.proto, tt.wantInner.srcPort, tt.wantInner.dstPort)
		})
	}
}

func TestBuiltinProcessBytesExtractsStackedEncapsulations(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})
	inner := ipv4Packet(6, [4]byte{192, 0, 2, 1}, [4]byte{198, 51, 100, 2}, tcpHeader(12345, 443))
	gre := grePayload(0x0800, inner)
	outer := ipv4Packet(47, [4]byte{203, 0, 113, 1}, [4]byte{203, 0, 113, 2}, gre)
	mpls := mplsPayload(17, true, outer)
	pppoe := pppoePayload(0x0281, mpls)
	packet := ethernetPayload(0x8100, dot1qPayload(100, 0x8864, pppoe))

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: packet,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	layers, ok := fields["packet_layers"].([]string)
	if !ok {
		t.Fatalf("expected packet_layers to be []string, got %T", fields["packet_layers"])
	}
	expectedLayers := []string{"ethernet", "dot1q", "pppoe", "mpls", "ipv4", "gre", "ipv4", "tcp"}
	if len(layers) != len(expectedLayers) {
		t.Fatalf("expected %d packet layers, got %#v", len(expectedLayers), layers)
	}
	for i, want := range expectedLayers {
		if layers[i] != want {
			t.Fatalf("expected packet_layers[%d]=%q, got %#v", i, want, layers[i])
		}
	}
	if got := fields["vlan_id"]; got != uint32(100) {
		t.Fatalf("expected vlan_id=100, got %#v", got)
	}
	expectIPLayer(t, fields, 0, "outer", "203.0.113.1", "203.0.113.2", 47, 0, 0)
	expectIPLayer(t, fields, 1, "inner", "192.0.2.1", "198.51.100.2", 6, 12345, 443)
}

func TestBuiltinProcessBytesExtractsMPLSInnerPacket(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})

	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x88, 0x47,
		0x00, 0x01, 0x11, 0x40,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x02, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	events, err := proc.Process(&event.Event{
		Source:  event.SourceMetadata{Type: "bytes"},
		Payload: packet,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields

	layers, ok := fields["packet_layers"].([]string)
	if !ok {
		t.Fatalf("expected packet_layers to be []string, got %T", fields["packet_layers"])
	}
	expectedLayers := []string{"ethernet", "mpls", "ipv4", "tcp"}
	if len(layers) != len(expectedLayers) {
		t.Fatalf("expected %d packet layers, got %#v", len(expectedLayers), layers)
	}
	for i, want := range expectedLayers {
		if layers[i] != want {
			t.Fatalf("expected packet_layers[%d]=%q, got %#v", i, want, layers[i])
		}
	}
	expectIPLayer(t, fields, 0, "single", "192.0.2.1", "198.51.100.2", 6, 12345, 443)
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected src_addr=192.0.2.1, got %#v", got)
	}
	if got := fields["dst_addr"]; got != "198.51.100.2" {
		t.Fatalf("expected dst_addr=198.51.100.2, got %#v", got)
	}
}

func TestBuiltinProcessReFlowJSONPreservesCounterFields(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})

	msg, err := json.Marshal(map[string]any{
		"record_kind":         "interface_counter",
		"agent_ip":            "192.0.2.10",
		"sub_agent_id":        3,
		"source_id":           4,
		"if_index":            5,
		"if_in_octets":        1234,
		"if_out_octets":       5678,
		"if_out_errors":       9,
		"if_status":           3,
		"if_direction":        1,
		"if_promiscuous_mode": 0,
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	events, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{
			Type: "json",
			JSON: event.JSONMetadata{Flavor: "reflow"},
		},
		Message: msg,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	fields := events[0].Fields
	if got := fields["record_kind"]; got != "interface_counter" {
		t.Fatalf("expected record_kind=interface_counter, got %#v", got)
	}
	if got := fields["if_index"]; got != float64(5) {
		t.Fatalf("expected if_index=5, got %#v", got)
	}
	if events[0].SFlow == nil || events[0].SFlow.AgentIP != "192.0.2.10" {
		t.Fatalf("expected sflow metadata to be populated, got %#v", events[0].SFlow)
	}
}

func TestBuiltinProcessRawPacketHeaderRequiresExplicitFlavor(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})

	msg, err := json.Marshal(map[string]any{
		"agent_ip":        "127.0.0.1",
		"sub_agent_id":    1,
		"source_id":       1,
		"sampling_rate":   100,
		"sample_pool":     1000,
		"drops":           0,
		"input_if":        10,
		"output_if":       20,
		"protocol":        1,
		"frame_length":    74,
		"stripped":        0,
		"original_length": 54,
		"header_hex":      "00112233445566778899aabb0800450000281234400040060000c0000201c6336401303901bb00000001000000005002200000000000",
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	events, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{
			Type: "json",
			JSON: event.JSONMetadata{Flavor: "reflow"},
		},
		Message: msg,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	fields := events[0].Fields
	if got := fields["header_hex"]; got == nil {
		t.Fatalf("expected header_hex to remain a canonical field when flavor=reflow")
	}
	if _, ok := fields["header_data"]; ok {
		t.Fatalf("did not expect raw packet header decoding for flavor=reflow")
	}

	events, err = proc.Process(&event.Event{
		Source: event.SourceMetadata{
			Type: "json",
			JSON: event.JSONMetadata{Flavor: "raw_packet_header"},
		},
		Message: msg,
	})
	if err != nil {
		t.Fatalf("Process returned error for raw_packet_header flavor: %v", err)
	}
	if _, ok := events[0].Fields["header_data"]; !ok {
		t.Fatalf("expected raw_packet_header flavor to decode header_data")
	}
	if got := events[0].Fields["bytes"]; got != int64(74) {
		t.Fatalf("expected bytes to use frame_length=74, got %#v", got)
	}
}

func TestBuiltinProcessFlowDoesNotTreatGenericProtocolAsSFlowHeaderProtocol(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})
	header := []byte{
		0x45, 0x00, 0x00, 0x28, 0x12, 0x34, 0x40, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x01,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x00,
		0x50, 0x02, 0x20, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	events, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{Type: "flow"},
		Fields: map[string]any{
			"flow_type":       "ipfix",
			"record_kind":     "packet",
			"protocol":        uint32(11),
			"frame_length":    uint32(74),
			"original_length": uint32(len(header)),
			"header_data":     header,
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if got := events[0].Fields["src_addr"]; got != nil {
		t.Fatalf("did not expect generic protocol field to drive packet parsing, got src_addr=%#v", got)
	}
}

func expectIPLayer(t *testing.T, fields map[string]any, index int, role, srcAddr, dstAddr string, proto, srcPort, dstPort uint32) {
	t.Helper()
	layers, ok := fields["ip_layers"].([]map[string]any)
	if !ok {
		t.Fatalf("expected ip_layers to be []map[string]any, got %T", fields["ip_layers"])
	}
	if count := fields["ip_layer_count"]; count != uint32(len(layers)) {
		t.Fatalf("expected ip_layer_count=%d, got %#v", len(layers), count)
	}
	if index < 0 || index >= len(layers) {
		t.Fatalf("expected ip_layers[%d], got %#v", index, layers)
	}
	layer := layers[index]
	if got := layer["role"]; got != role {
		t.Fatalf("expected ip_layers[%d].role=%q, got %#v", index, role, got)
	}
	if got := layer["src_addr"]; got != srcAddr {
		t.Fatalf("expected ip_layers[%d].src_addr=%q, got %#v", index, srcAddr, got)
	}
	if got := layer["dst_addr"]; got != dstAddr {
		t.Fatalf("expected ip_layers[%d].dst_addr=%q, got %#v", index, dstAddr, got)
	}
	if got := layer["proto"]; got != proto {
		t.Fatalf("expected ip_layers[%d].proto=%d, got %#v", index, proto, got)
	}
	if got := layer["src_port"]; got != srcPort {
		t.Fatalf("expected ip_layers[%d].src_port=%d, got %#v", index, srcPort, got)
	}
	if got := layer["dst_port"]; got != dstPort {
		t.Fatalf("expected ip_layers[%d].dst_port=%d, got %#v", index, dstPort, got)
	}
}

type packetLayerExpectation struct {
	role    string
	srcAddr string
	dstAddr string
	proto   uint32
	srcPort uint32
	dstPort uint32
}

func containsLayer(layers []string, want string) bool {
	for _, layer := range layers {
		if layer == want {
			return true
		}
	}
	return false
}

func vxlanTestPacket(dstPort uint16) []byte {
	packet := []byte{
		0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
		0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x5c, 0x00, 0x01, 0x00, 0x00, 0x40, 0x11, 0x00, 0x00,
		0xcb, 0x00, 0x71, 0x01,
		0xcb, 0x00, 0x71, 0x02,
		0x12, 0x34, 0x12, 0xb5, 0x00, 0x48, 0x00, 0x00,
		0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x64, 0x00,
		0xde, 0xad, 0xbe, 0xef, 0x00, 0x01,
		0xde, 0xad, 0xbe, 0xef, 0x00, 0x02,
		0x08, 0x00,
		0x45, 0x00, 0x00, 0x28, 0x00, 0x02, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
		0xc0, 0x00, 0x02, 0x01,
		0xc6, 0x33, 0x64, 0x02,
		0x30, 0x39, 0x01, 0xbb,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	packet[36] = byte(dstPort >> 8)
	packet[37] = byte(dstPort)
	return packet
}

func ethernetPayload(etherType uint16, payload []byte) []byte {
	out := make([]byte, 14+len(payload))
	copy(out[0:6], []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55})
	copy(out[6:12], []byte{0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb})
	binary.BigEndian.PutUint16(out[12:14], etherType)
	copy(out[14:], payload)
	return out
}

func dot1qPayload(vlanID uint16, etherType uint16, payload []byte) []byte {
	out := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(out[0:2], vlanID&0x0fff)
	binary.BigEndian.PutUint16(out[2:4], etherType)
	copy(out[4:], payload)
	return out
}

func ipv4Packet(proto byte, src, dst [4]byte, payload []byte) []byte {
	out := make([]byte, 20+len(payload))
	out[0] = 0x45
	binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
	out[8] = 64
	out[9] = proto
	copy(out[12:16], src[:])
	copy(out[16:20], dst[:])
	copy(out[20:], payload)
	return out
}

func ipv6RoutingPacket(payload []byte) []byte {
	out := make([]byte, 48+len(payload))
	out[0] = 0x60
	binary.BigEndian.PutUint16(out[4:6], uint16(8+len(payload)))
	out[6] = 43
	out[7] = 64
	copy(out[8:24], []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1})
	copy(out[24:40], []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2})
	out[40] = 6
	out[41] = 0
	copy(out[48:], payload)
	return out
}

func tcpHeader(srcPort, dstPort uint16) []byte {
	out := make([]byte, 20)
	binary.BigEndian.PutUint16(out[0:2], srcPort)
	binary.BigEndian.PutUint16(out[2:4], dstPort)
	out[12] = 0x50
	out[13] = 0x02
	return out
}

func udpPacket(srcPort, dstPort uint16, payload []byte) []byte {
	out := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(out[0:2], srcPort)
	binary.BigEndian.PutUint16(out[2:4], dstPort)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(out)))
	copy(out[8:], payload)
	return out
}

func l2tpPayload(inner []byte) []byte {
	out := make([]byte, 10+len(inner))
	binary.BigEndian.PutUint16(out[0:2], 0x0002)
	binary.BigEndian.PutUint16(out[2:4], 1)
	binary.BigEndian.PutUint16(out[4:6], 2)
	out[6] = 0xff
	out[7] = 0x03
	binary.BigEndian.PutUint16(out[8:10], 0x0021)
	copy(out[10:], inner)
	return out
}

func gtpuPayload(inner []byte) []byte {
	out := make([]byte, 8+len(inner))
	out[0] = 0x30
	out[1] = 0xff
	binary.BigEndian.PutUint16(out[2:4], uint16(len(inner)))
	binary.BigEndian.PutUint32(out[4:8], 0x01020304)
	copy(out[8:], inner)
	return out
}

func grePayload(proto uint16, inner []byte) []byte {
	out := make([]byte, 4+len(inner))
	binary.BigEndian.PutUint16(out[2:4], proto)
	copy(out[4:], inner)
	return out
}

func mplsPayload(label uint32, bos bool, inner []byte) []byte {
	out := make([]byte, 4+len(inner))
	raw := (label & 0xfffff) << 12
	if bos {
		raw |= 1 << 8
	}
	raw |= 64
	binary.BigEndian.PutUint32(out[0:4], raw)
	copy(out[4:], inner)
	return out
}

func pppoePayload(proto uint16, inner []byte) []byte {
	out := make([]byte, 8+len(inner))
	out[0] = 0x11
	binary.BigEndian.PutUint16(out[2:4], 1)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(inner)+2))
	binary.BigEndian.PutUint16(out[6:8], proto)
	copy(out[8:], inner)
	return out
}
