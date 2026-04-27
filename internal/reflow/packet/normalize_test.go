package packet

import (
	"encoding/hex"
	"testing"

	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func TestNormalizeEventParsesDirectIPv4SampledHeader(t *testing.T) {
	header := mustDecodeHex(t, "450000281234400040060000c0000201c6336401303901bb00000001000000005002200000000000")
	evt := &event.Event{
		Fields: map[string]any{
			"protocol":        uint32(253),
			"frame_length":    uint32(74),
			"original_length": uint32(len(header)),
			"header_data":     header,
		},
	}

	if err := NormalizeEvent(evt, NormalizeOptions{HeaderProtocol: 11}); err != nil {
		t.Fatalf("NormalizeEvent returned error: %v", err)
	}

	if got := evt.Fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected src_addr=192.0.2.1, got %#v", got)
	}
	if got := evt.Fields["dst_addr"]; got != "198.51.100.1" {
		t.Fatalf("expected dst_addr=198.51.100.1, got %#v", got)
	}
	if got := evt.Fields["src_port"]; got != uint32(12345) {
		t.Fatalf("expected src_port=12345, got %#v", got)
	}
	if got := evt.Fields["dst_port"]; got != uint32(443) {
		t.Fatalf("expected dst_port=443, got %#v", got)
	}
	if got := evt.Fields["bytes"]; got != int64(74) {
		t.Fatalf("expected bytes to use frame_length=74, got %#v", got)
	}
}

func TestNormalizeEventParsesDirectIPv6SampledHeader(t *testing.T) {
	header := []byte{
		0x60, 0x00, 0x00, 0x00, 0x00, 0x08, 0x11, 0x40,
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x20, 0x01, 0x0d, 0xb8, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02,
		0x30, 0x39, 0x01, 0xbb, 0x00, 0x08, 0x00, 0x00,
	}
	evt := &event.Event{
		Fields: map[string]any{
			"protocol":        uint32(253),
			"frame_length":    uint32(88),
			"original_length": uint32(len(header)),
			"header_data":     header,
		},
	}

	if err := NormalizeEvent(evt, NormalizeOptions{HeaderProtocol: 12}); err != nil {
		t.Fatalf("NormalizeEvent returned error: %v", err)
	}

	if got := evt.Fields["src_addr"]; got != "2001:db8::1" {
		t.Fatalf("expected src_addr=2001:db8::1, got %#v", got)
	}
	if got := evt.Fields["dst_addr"]; got != "2001:db8::2" {
		t.Fatalf("expected dst_addr=2001:db8::2, got %#v", got)
	}
	if got := evt.Fields["src_port"]; got != uint32(12345) {
		t.Fatalf("expected src_port=12345, got %#v", got)
	}
	if got := evt.Fields["dst_port"]; got != uint32(443) {
		t.Fatalf("expected dst_port=443, got %#v", got)
	}
}

func TestNormalizeEventDoesNotTreatProtocolFieldAsHeaderProtocol(t *testing.T) {
	header := mustDecodeHex(t, "450000281234400040060000c0000201c6336401303901bb00000001000000005002200000000000")
	evt := &event.Event{
		Fields: map[string]any{
			"protocol":        uint32(11),
			"frame_length":    uint32(74),
			"original_length": uint32(len(header)),
			"header_data":     header,
		},
	}

	if err := NormalizeEvent(evt, NormalizeOptions{}); err != nil {
		t.Fatalf("NormalizeEvent returned error: %v", err)
	}
	if got := evt.Fields["src_addr"]; got != nil {
		t.Fatalf("did not expect generic protocol field to drive packet parsing, got src_addr=%#v", got)
	}
}

func mustDecodeHex(t *testing.T, raw string) []byte {
	t.Helper()
	out, err := hex.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return out
}
