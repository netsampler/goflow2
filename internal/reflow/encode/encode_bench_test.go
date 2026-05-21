package encode

import (
	"bytes"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func BenchmarkEncode(b *testing.B) {
	benchmarks := []struct {
		name string
		cfg  config.EncoderConfig
		evt  *event.Event
	}{
		{
			name: "json_canonical",
			cfg:  config.EncoderConfig{Type: "json"},
			evt:  benchFlowEvent(),
		},
		{
			name: "json_goflow2v2",
			cfg: config.EncoderConfig{
				Type: "json",
				JSON: config.JSONConfig{Flavor: "goflow2v2"},
			},
			evt: benchFlowEvent(),
		},
		{
			name: "protobuf_canonical",
			cfg: config.EncoderConfig{
				Type:     "protobuf",
				Protobuf: config.ProtobufConfig{Flavor: "canonical"},
			},
			evt: benchFlowEvent(),
		},
		{
			name: "sflow_sampled_header",
			cfg:  config.EncoderConfig{Type: "sflow"},
			evt:  benchSFlowPacketEvent(),
		},
		{
			name: "ipfix_templated",
			cfg:  benchTemplatedFlowConfig("ipfix"),
			evt:  benchFlowEvent(),
		},
		{
			name: "netflowv9_templated",
			cfg:  benchTemplatedFlowConfig("netflowv9"),
			evt:  benchFlowEvent(),
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			enc, err := New(bm.cfg)
			if err != nil {
				b.Fatalf("New returned error: %v", err)
			}
			if _, err := enc.Encode(bm.evt); err != nil {
				b.Fatalf("warm Encode returned error: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				payloads, err := enc.Encode(bm.evt)
				if err != nil {
					b.Fatalf("Encode returned error: %v", err)
				}
				if len(payloads) == 0 {
					b.Fatalf("expected at least one payload")
				}
			}
		})
	}
}

func BenchmarkEncodeSFlowBatch(b *testing.B) {
	enc := NewSFlowEncoder(config.EncoderConfig{
		Type: "sflow",
		Batch: config.BatchConfig{
			Enabled:    boolPtr(true),
			MaxRecords: 32,
		},
	})
	evt := benchSFlowPacketEvent()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := enc.Encode(evt); err != nil {
			b.Fatalf("Encode returned error: %v", err)
		}
	}
	if _, err := enc.Flush(); err != nil {
		b.Fatalf("Flush returned error: %v", err)
	}
}

func BenchmarkEncodeJSONDropFields(b *testing.B) {
	evt := benchFlowEvent()
	evt.Fields["header_data"] = bytes.Repeat([]byte{0xaa}, 128)
	enc := NewJSONEncoder(config.EncoderConfig{
		Type: "json",
		JSON: config.JSONConfig{
			DropFields: []string{"header_data"},
		},
	})

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		payloads, err := enc.Encode(evt)
		if err != nil {
			b.Fatalf("Encode returned error: %v", err)
		}
		if len(payloads) != 1 {
			b.Fatalf("expected 1 payload, got %d", len(payloads))
		}
	}
}

func benchFlowEvent() *event.Event {
	return &event.Event{
		ReceivedAt: time.Unix(1_700_000_001, 0).UTC(),
		Source: event.SourceMetadata{
			AgentIP: "192.0.2.1",
			Sampling: &event.SamplingMetadata{
				Rate: 100,
			},
		},
		Fields: map[string]any{
			"flow_type":          "sflow",
			"agent_ip":           "192.0.2.1",
			"source_id":          uint32(42),
			"src_addr":           "192.0.2.10",
			"dst_addr":           "198.51.100.20",
			"src_port":           uint32(12345),
			"dst_port":           uint32(443),
			"proto":              uint32(6),
			"bytes":              int64(321),
			"packets":            int64(7),
			"input_if":           uint32(9),
			"output_if":          uint32(10),
			"start_time_unix":    int64(1_700_000_000_100),
			"end_time_unix":      int64(1_700_000_000_900),
			"time_flow_start_ns": int64(1_700_000_000_100_123_456),
			"time_flow_end_ns":   int64(1_700_000_000_900_123_456),
		},
	}
}

func benchSFlowPacketEvent() *event.Event {
	evt := benchFlowEvent()
	evt.Fields["protocol"] = uint32(1)
	evt.Fields["frame_length"] = uint32(60)
	evt.Fields["original_length"] = uint32(60)
	evt.Fields["header_data"] = []byte{
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
	evt.SFlow = &event.SFlowMetadata{
		AgentIP:      "192.0.2.1",
		SourceID:     42,
		SamplingRate: 100,
	}
	return evt
}

func benchTemplatedFlowConfig(typ string) config.EncoderConfig {
	cfg := testTFlowEncoderConfig(typ)
	cfg.TemplatedFlow.TemplateBaseID = 256
	cfg.TemplatedFlow.TemplateRefresh = 0
	cfg.TemplatedFlow.OptionsRefresh = 0
	return cfg
}

func boolPtr(v bool) *bool {
	return &v
}
