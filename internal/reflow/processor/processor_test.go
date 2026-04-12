package processor

import (
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
