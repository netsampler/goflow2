package processor

import (
	"encoding/json"
	"testing"

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
	if got := fields["tunnel_type"]; got != "gre" {
		t.Fatalf("expected tunnel_type=gre, got %#v", got)
	}
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
	if got := fields["tunnel_type"]; got != "vxlan" {
		t.Fatalf("expected tunnel_type=vxlan, got %#v", got)
	}
	if got := fields["outer_src_addr"]; got != "203.0.113.1" {
		t.Fatalf("expected outer_src_addr=203.0.113.1, got %#v", got)
	}
	if got := fields["outer_dst_addr"]; got != "203.0.113.2" {
		t.Fatalf("expected outer_dst_addr=203.0.113.2, got %#v", got)
	}
	if got := fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected inner src_addr=192.0.2.1, got %#v", got)
	}
	if got := fields["dst_addr"]; got != "198.51.100.2" {
		t.Fatalf("expected inner dst_addr=198.51.100.2, got %#v", got)
	}
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
	if got := fields["tunnel_type"]; got != "mpls" {
		t.Fatalf("expected tunnel_type=mpls, got %#v", got)
	}
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
