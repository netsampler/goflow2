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
	if got := events[0].Fields["message_type"]; got != "flow" {
		t.Fatalf("expected message_type=flow, got %#v", got)
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
	if got := fields["message_type"]; got != "bytes" {
		t.Fatalf("expected message_type=bytes, got %#v", got)
	}
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

func TestBuiltinProcessGoFlow2V2BuildsPseudoPacket(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			BuildPseudoPacket: true,
		},
	})

	msg, err := json.Marshal(map[string]any{
		"sampler_address": "192.0.2.10",
		"src_addr":        "wAACAQ==",
		"dst_addr":        "xjNkAg==",
		"src_port":        12345,
		"dst_port":        53,
		"proto":           17,
	})
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	events, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{
			Type: "json",
			JSON: event.JSONMetadata{Flavor: "goflow2v2"},
		},
		Message: msg,
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	fields := events[0].Fields
	headerData, ok := fields["header_data"].([]byte)
	if !ok {
		t.Fatalf("expected header_data to be []byte, got %T", fields["header_data"])
	}
	if len(headerData) == 0 {
		t.Fatalf("expected pseudo packet bytes to be present")
	}
	if got := fields["protocol"]; got != uint32(1) {
		t.Fatalf("expected protocol=1 for pseudo Ethernet frame, got %#v", got)
	}
	if got := fields["frame_length"]; got != uint32(len(headerData)) {
		t.Fatalf("expected frame_length=%d, got %#v", len(headerData), got)
	}
}

func TestBuiltinProcessReFlowJSONPreservesCounterFields(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{})

	msg, err := json.Marshal(map[string]any{
		"message_type":    "counter",
		"record_kind":     "interface_counter",
		"agent_ip":        "192.0.2.10",
		"sub_agent_id":    3,
		"source_id":       4,
		"if_index":        5,
		"if_in_octets":    1234,
		"if_out_octets":   5678,
		"if_out_errors":   9,
		"if_status":       3,
		"if_direction":    1,
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
	if got := fields["message_type"]; got != "counter" {
		t.Fatalf("expected message_type=counter, got %#v", got)
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
}
