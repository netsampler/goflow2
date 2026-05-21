package processor

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func BenchmarkBuiltinProcess(b *testing.B) {
	benchmarks := []struct {
		name string
		proc *Builtin
		new  func() *event.Event
	}{
		{
			name: "bytes_ipv4_tcp",
			proc: NewBuiltin(config.ProcessorConfig{}),
			new: func() *event.Event {
				packet := benchIPv4TCPPacket()
				return &event.Event{
					ReceivedAt: benchEventTime(),
					Source:     event.SourceMetadata{Type: "bytes"},
					Payload:    packet,
				}
			},
		},
		{
			name: "bytes_vxlan",
			proc: NewBuiltin(config.ProcessorConfig{}),
			new: func() *event.Event {
				return &event.Event{
					ReceivedAt: benchEventTime(),
					Source:     event.SourceMetadata{Type: "bytes"},
					Payload:    vxlanTestPacket(4789),
				}
			},
		},
		{
			name: "goflow2v2_json",
			proc: NewBuiltin(config.ProcessorConfig{}),
			new: func() *event.Event {
				return &event.Event{
					ReceivedAt: benchEventTime(),
					Source: event.SourceMetadata{
						Type: "json",
						JSON: event.JSONMetadata{Flavor: "goflow2v2"},
					},
					Message: append([]byte(nil), benchGoFlow2V2JSON...),
				}
			},
		},
		{
			name: "reflow_json",
			proc: NewBuiltin(config.ProcessorConfig{}),
			new: func() *event.Event {
				return &event.Event{
					ReceivedAt: benchEventTime(),
					Source: event.SourceMetadata{
						Type: "json",
						JSON: event.JSONMetadata{Flavor: "reflow"},
					},
					Message: append([]byte(nil), benchReFlowJSON...),
				}
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				events, err := bm.proc.Process(bm.new())
				if err != nil {
					b.Fatalf("Process returned error: %v", err)
				}
				if len(events) != 1 {
					b.Fatalf("expected 1 event, got %d", len(events))
				}
			}
		})
	}
}

func benchIPv4TCPPacket() []byte {
	return ethernetPayload(
		0x0800,
		ipv4Packet(6, [4]byte{192, 0, 2, 1}, [4]byte{198, 51, 100, 2}, tcpHeader(12345, 443)),
	)
}

func benchEventTime() time.Time {
	return time.Unix(1_700_000_001, 123_000_000).UTC()
}

var benchGoFlow2V2JSON = []byte(`{
	"type": 1,
	"time_received_ns": 1700000001123000000,
	"time_flow_start_ns": 1700000000100123456,
	"time_flow_end_ns": 1700000000900123456,
	"sampler_address": "wAACAQ==",
	"src_addr": "wAACCg==",
	"dst_addr": "xjNkFA==",
	"src_port": 12345,
	"dst_port": 443,
	"proto": 6,
	"bytes": 321,
	"packets": 7,
	"in_if": 9,
	"out_if": 10,
	"sampling_rate": 100
}`)

var benchReFlowJSON = mustMarshalBenchReFlowJSON()

func mustMarshalBenchReFlowJSON() []byte {
	raw := map[string]any{
		"received_at": benchEventTime().Format(time.RFC3339Nano),
		"source": map[string]any{
			"type":    "flow",
			"network": "stream",
			"address": "-",
		},
		"fields": map[string]any{
			"flow_type":          "sflow",
			"record_kind":        "packet",
			"agent_ip":           "192.0.2.1",
			"src_addr":           "192.0.2.10",
			"dst_addr":           "198.51.100.20",
			"src_port":           12345,
			"dst_port":           443,
			"proto":              6,
			"bytes":              321,
			"packets":            7,
			"time_flow_start_ns": int64(1_700_000_000_100_123_456),
			"time_flow_end_ns":   int64(1_700_000_000_900_123_456),
			"protocol":           1,
			"frame_length":       len(benchIPv4TCPPacket()),
			"original_length":    len(benchIPv4TCPPacket()),
			"header_data":        benchIPv4TCPPacket(),
		},
	}
	out, err := json.Marshal(raw)
	if err != nil {
		panic(err)
	}
	return out
}
