package app

import (
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/decode"
	reflowencode "github.com/netsampler/goflow2/v3/internal/reflow/encode"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/internal/reflow/processor"
)

func BenchmarkReFlowRawIPFIXToEncode(b *testing.B) {
	catalog := benchReFlowIPFIXCatalog()
	templates, data := benchReFlowIPFIXPayloads(b, catalog)

	benchmarks := []struct {
		name string
		cfg  config.EncoderConfig
	}{
		{name: "protobuf", cfg: config.EncoderConfig{Type: "protobuf"}},
		{name: "json", cfg: config.EncoderConfig{Type: "json"}},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			dec := decode.NewWithCatalog(catalog)
			defer dec.Close()
			for _, payload := range templates {
				if _, err := dec.Decode(&event.Event{Source: event.SourceMetadata{Type: "flow"}, Payload: payload}); err != nil {
					b.Fatalf("decode template payload: %v", err)
				}
			}
			proc := processor.NewBuiltin(config.ProcessorConfig{})
			enc, err := reflowencode.New(bm.cfg)
			if err != nil {
				b.Fatalf("New encoder returned error: %v", err)
			}

			var totalBytes int
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				decoded, err := dec.Decode(&event.Event{
					ReceivedAt: time.Unix(1_700_000_001, 0).UTC(),
					Source:     event.SourceMetadata{Type: "flow"},
					Payload:    data,
				})
				if err != nil {
					b.Fatalf("Decode returned error: %v", err)
				}
				if len(decoded) != 1 {
					b.Fatalf("expected 1 decoded event, got %d", len(decoded))
				}
				processed, err := proc.Process(decoded[0])
				if err != nil {
					b.Fatalf("Process returned error: %v", err)
				}
				for _, evt := range processed {
					payloads, err := enc.Encode(evt)
					if err != nil {
						b.Fatalf("Encode returned error: %v", err)
					}
					for _, payload := range payloads {
						totalBytes += len(payload)
					}
				}
			}
			if totalBytes == 0 {
				b.Fatalf("expected encoded payload bytes")
			}
		})
	}
}

func benchReFlowIPFIXPayloads(b *testing.B, catalog map[string]config.IPFIXFieldDefinition) ([][]byte, []byte) {
	b.Helper()
	enc := reflowencode.NewIPFIXEncoder(config.EncoderConfig{
		Type: "ipfix",
		TemplatedFlow: config.TemplatedFlowConfig{
			TemplateBaseID: 256,
			Data: config.TemplatedFlowDataConfig{
				Select:  []string{"src_addr", "dst_addr", "src_port", "dst_port", "proto", "bytes", "packets", "input_if", "output_if", "start_time_unix", "end_time_unix"},
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
			Stream: "flow_data",
			FieldNames: []string{
				"src_addr",
				"dst_addr",
				"src_port",
				"dst_port",
				"proto",
				"bytes",
				"packets",
				"input_if",
				"output_if",
				"start_time_unix",
				"end_time_unix",
			},
		},
	})
	if err != nil {
		b.Fatalf("encode schema returned error: %v", err)
	}
	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: time.Unix(1_700_000_001, 0).UTC(),
		Stream:     "flow_data",
		Fields: map[string]any{
			"src_addr":        "192.0.2.10",
			"dst_addr":        "198.51.100.20",
			"src_port":        uint32(12345),
			"dst_port":        uint32(443),
			"proto":           uint32(6),
			"bytes":           int64(321),
			"packets":         int64(7),
			"input_if":        uint32(9),
			"output_if":       uint32(10),
			"start_time_unix": int64(1_700_000_000_100),
			"end_time_unix":   int64(1_700_000_000_900),
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

func benchReFlowIPFIXCatalog() map[string]config.IPFIXFieldDefinition {
	return map[string]config.IPFIXFieldDefinition{
		"src_addr":        {ID: 8, Length: 4, Type: "ipv4Address"},
		"dst_addr":        {ID: 12, Length: 4, Type: "ipv4Address"},
		"src_port":        {ID: 7, Length: 2, Type: "unsigned16"},
		"dst_port":        {ID: 11, Length: 2, Type: "unsigned16"},
		"proto":           {ID: 4, Length: 1, Type: "unsigned8"},
		"bytes":           {ID: 1, Length: 8, Type: "unsigned64"},
		"packets":         {ID: 2, Length: 4, Type: "unsigned32"},
		"input_if":        {ID: 10, Length: 4, Type: "unsigned32"},
		"output_if":       {ID: 14, Length: 4, Type: "unsigned32"},
		"start_time_unix": {ID: 152, Length: 8, Type: "unsigned64"},
		"end_time_unix":   {ID: 153, Length: 8, Type: "unsigned64"},
	}
}
