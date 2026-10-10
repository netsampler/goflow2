package decode

import (
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/encode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func BenchmarkDecode(b *testing.B) {
	b.Run("bytes_passthrough", func(b *testing.B) {
		dec := New()
		defer dec.Close()
		packet := []byte{
			0x00, 0x11, 0x22, 0x33, 0x44, 0x55,
			0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
			0x08, 0x00,
			0x45, 0x00, 0x00, 0x28, 0x00, 0x00, 0x00, 0x00, 0x40, 0x06, 0x00, 0x00,
			0xc0, 0x00, 0x02, 0x01,
			0xc6, 0x33, 0x64, 0x14,
			0x30, 0x39, 0x01, 0xbb,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x50, 0x02, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		}
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			events, err := dec.Decode(&event.Event{
				ReceivedAt: time.Unix(1_700_000_001, 0).UTC(),
				Source:     event.SourceMetadata{Type: "bytes"},
				Payload:    packet,
			})
			if err != nil {
				b.Fatalf("Decode returned error: %v", err)
			}
			if len(events) != 1 {
				b.Fatalf("expected 1 event, got %d", len(events))
			}
		}
	})

	b.Run("ipfix_data", func(b *testing.B) {
		catalog := benchDecodeCatalog()
		templates, data := benchIPFIXPayloads(b, catalog)
		dec := NewWithCatalog(catalog)
		defer dec.Close()
		for _, payload := range templates {
			if _, err := dec.Decode(&event.Event{Source: event.SourceMetadata{Type: "flow"}, Payload: payload}); err != nil {
				b.Fatalf("decode template payload: %v", err)
			}
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			events, err := dec.Decode(&event.Event{
				ReceivedAt: time.Unix(1_700_000_001, 0).UTC(),
				Source:     event.SourceMetadata{Type: "flow"},
				Payload:    data,
			})
			if err != nil {
				b.Fatalf("Decode returned error: %v", err)
			}
			if len(events) != 1 {
				b.Fatalf("expected 1 data event, got %d", len(events))
			}
		}
	})
}

func benchIPFIXPayloads(b *testing.B, catalog map[string]config.IPFIXFieldDefinition) ([][]byte, []byte) {
	b.Helper()
	enc := encode.NewIPFIXEncoder(config.EncoderConfig{
		Type: "ipfix",
		TemplatedFlow: config.TemplatedFlowConfig{
			TemplateBaseID: 256,
			Data: config.TemplatedFlowDataConfig{
				Select:  []string{"src_addr", "dst_addr", "src_port", "dst_port", "proto", "bytes", "packets"},
				Catalog: catalog,
			},
		},
	})
	templates, err := enc.Encode(&event.Event{
		Kind:   "control",
		Stream: "flow_data",
		Control: &event.ControlMetadata{
			Type:   "schema",
			Stream: "flow_data",
		},
		Payload: event.AggregationSchema{
			Stream:     "flow_data",
			FieldNames: []string{"src_addr", "dst_addr", "src_port", "dst_port", "proto", "bytes", "packets"},
		},
	})
	if err != nil {
		b.Fatalf("encode schema returned error: %v", err)
	}
	if len(templates) == 0 {
		b.Fatalf("expected template payloads")
	}
	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: time.Unix(1_700_000_001, 0).UTC(),
		Stream:     "flow_data",
		Fields: map[string]any{
			"src_addr": "192.0.2.10",
			"dst_addr": "198.51.100.20",
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"proto":    uint32(6),
			"bytes":    int64(321),
			"packets":  int64(7),
		},
	})
	if err != nil {
		b.Fatalf("Encode returned error: %v", err)
	}
	if len(payloads) != 1 {
		b.Fatalf("expected one data payload, got %d", len(payloads))
	}
	return templates, payloads[0]
}

func benchDecodeCatalog() map[string]config.IPFIXFieldDefinition {
	return map[string]config.IPFIXFieldDefinition{
		"src_addr": {ID: 8, Length: 4, Type: "ipv4Address"},
		"dst_addr": {ID: 12, Length: 4, Type: "ipv4Address"},
		"src_port": {ID: 7, Length: 2, Type: "unsigned16"},
		"dst_port": {ID: 11, Length: 2, Type: "unsigned16"},
		"proto":    {ID: 4, Length: 1, Type: "unsigned8"},
		"bytes":    {ID: 1, Length: 8, Type: "unsigned64"},
		"packets":  {ID: 2, Length: 8, Type: "unsigned64"},
	}
}
