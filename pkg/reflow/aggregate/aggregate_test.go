package aggregate

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/processor"
)

func TestStatefulFlushSumsPacketCounters(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	first := &event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(60),
			"packets":  int64(1),
		},
	}
	second := &event.Event{
		ReceivedAt: time.Unix(2, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(70),
			"packets":  int64(1),
		},
	}

	if out, err := agg.Process(first); err != nil || len(out) != 0 {
		t.Fatalf("first Process returned out=%d err=%v", len(out), err)
	}
	if out, err := agg.Process(second); err != nil || len(out) != 0 {
		t.Fatalf("second Process returned out=%d err=%v", len(out), err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if got := out[0].Fields["packets"]; got != int64(2) {
		t.Fatalf("expected packets=2, got %#v", got)
	}
	if got := out[0].Fields["bytes"]; got != int64(130) {
		t.Fatalf("expected bytes=130, got %#v", got)
	}
}

func TestStatefulWarnsAndDropsEventWithMissingKey(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	agg, err := New(config.AggregatorConfig{
		Stream:    "interface_options",
		KeyFields: []string{"observation_domain_id", "input_if"},
		Current:   []string{"interface_name"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	out, err := agg.Process(&event.Event{
		Fields: map[string]any{
			"observation_domain_id": uint32(777),
			"interface_name":        "eth0",
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected missing-key event to be dropped, got %d events", len(out))
	}

	got := logs.String()
	for _, want := range []string{
		"dropping aggregate event with missing key field",
		"stream=interface_options",
		"key=input_if",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected log to contain %q, got %q", want, got)
		}
	}
}

func TestStatefulAggregatesNestedPacketLayerFields(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		KeyFields: []string{
			"outer_src_addr",
			"src_addr",
			"dst_addr",
			"proto",
			"src_port",
			"dst_port",
		},
		Sum: []string{"bytes", "packets"},
		Current: []string{
			"outer_dst_addr",
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	fields := map[string]any{
		"outer_src_addr": "203.0.113.1",
		"outer_dst_addr": "203.0.113.2",
		"outer_proto":    uint32(47),
		"src_addr":       "192.0.2.1",
		"dst_addr":       "198.51.100.2",
		"proto":          uint32(6),
		"src_port":       uint32(12345),
		"dst_port":       uint32(443),
		"bytes":          int64(60),
		"packets":        int64(1),
	}
	if _, err := agg.Process(&event.Event{Fields: fields}); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if got := out[0].Fields["outer_src_addr"]; got != "203.0.113.1" {
		t.Fatalf("expected outer source key to be preserved, got %#v", got)
	}
	if got := out[0].Fields["src_addr"]; got != "192.0.2.1" {
		t.Fatalf("expected inner source key to be preserved, got %#v", got)
	}
	if got := out[0].Fields["outer_dst_addr"]; got != "203.0.113.2" {
		t.Fatalf("expected current nested field to be preserved, got %#v", got)
	}
	if got := out[0].Fields["bytes"]; got != int64(60) {
		t.Fatalf("expected bytes=60, got %#v", got)
	}
}

func TestStatefulAggregatesNestedReFlowJSON(t *testing.T) {
	cfg := nestedReFlowJSONConfig()
	proc, err := processor.New(cfg.Processor)
	if err != nil {
		t.Fatalf("processor.New returned error: %v", err)
	}

	tests := []struct {
		name        string
		raw         string
		wantBytes   int64
		wantLayers  []string
		wantVLANID  any
		wantMPLS    any
		wantEther   any
		wantAggKey  string
		wantAggVal  any
		wantAggKey2 string
		wantAggVal2 any
	}{
		{
			name:        "nested ip layers",
			raw:         nestedIPLayersJSON,
			wantBytes:   96,
			wantAggKey:  "outer_src_addr",
			wantAggVal:  "203.0.113.1",
			wantAggKey2: "src_addr",
			wantAggVal2: "192.0.2.1",
		},
		{
			name:       "dot1q mpls gre",
			raw:        nestedDot1QMPLSGREJSON,
			wantBytes:  128,
			wantLayers: []string{"ethernet", "dot1q", "mpls", "ipv4", "gre", "ipv4", "tcp"},
			wantVLANID: uint32(100),
			wantAggKey: "outer_dst_addr",
			wantAggVal: "203.0.113.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := proc.Process(&event.Event{
				Source: event.SourceMetadata{
					Type: cfg.Sources[0].Type,
					JSON: event.JSONMetadata{
						Flavor: cfg.Sources[0].JSON.Flavor,
					},
				},
				Message: []byte(tt.raw),
			})
			if err != nil {
				t.Fatalf("Process ReFlow JSON returned error: %v", err)
			}
			if len(events) != 1 {
				t.Fatalf("expected 1 processed event, got %d", len(events))
			}
			if events[0].Packet == nil || len(events[0].Packet.Layers) == 0 {
				t.Fatalf("expected sample JSON to populate packet.layers")
			}
			if len(tt.wantLayers) > 0 {
				layers := packetLayerKinds(events[0].Packet)
				if len(layers) != len(tt.wantLayers) {
					t.Fatalf("expected packet layer count %d, got %#v", len(tt.wantLayers), layers)
				}
				for i, want := range tt.wantLayers {
					if layers[i] != want {
						t.Fatalf("expected packet.layers[%d].kind=%q, got %#v", i, want, layers[i])
					}
				}
			}
			if tt.wantVLANID != nil {
				if got := events[0].Fields["vlan_id"]; got != tt.wantVLANID {
					t.Fatalf("expected vlan_id=%#v, got %#v", tt.wantVLANID, got)
				}
			}
			if tt.wantMPLS != nil {
				if got := events[0].Fields["mpls_label"]; got != tt.wantMPLS {
					t.Fatalf("expected mpls_label=%#v, got %#v", tt.wantMPLS, got)
				}
			}
			if tt.wantEther != nil {
				if got := events[0].Fields["ether_type"]; got != tt.wantEther {
					t.Fatalf("expected ether_type=%#v, got %#v", tt.wantEther, got)
				}
			}

			agg, err := New(cfg.Aggregators[0])
			if err != nil {
				t.Fatalf("New returned error: %v", err)
			}
			if _, err := agg.Process(events[0]); err != nil {
				t.Fatalf("Aggregate sample event returned error: %v", err)
			}
			out, err := agg.Close()
			if err != nil {
				t.Fatalf("Close returned error: %v", err)
			}
			if len(out) != 1 {
				t.Fatalf("expected 1 aggregate output, got %d", len(out))
			}
			if out[0].Stream != "nested_flow_data" {
				t.Fatalf("expected nested_flow_data stream, got %q", out[0].Stream)
			}
			if got := out[0].Fields[tt.wantAggKey]; got != tt.wantAggVal {
				t.Fatalf("expected %s=%#v, got %#v", tt.wantAggKey, tt.wantAggVal, got)
			}
			if tt.wantAggKey2 != "" {
				if got := out[0].Fields[tt.wantAggKey2]; got != tt.wantAggVal2 {
					t.Fatalf("expected %s=%#v, got %#v", tt.wantAggKey2, tt.wantAggVal2, got)
				}
			}
			if got := out[0].Fields["bytes"]; got != tt.wantBytes {
				t.Fatalf("expected bytes=%d, got %#v", tt.wantBytes, got)
			}
		})
	}
}

func nestedReFlowJSONConfig() *config.Config {
	return &config.Config{
		Sources: []config.SourceConfig{
			{
				Type: "json",
				JSON: config.JSONConfig{Flavor: "reflow"},
			},
		},
		Processor: config.ProcessorConfig{
			Type:    "builtin",
			Workers: 1,
			Builtin: config.BuiltinProcessorConfig{
				DropMessage: true,
				DropPayload: true,
			},
		},
		Aggregators: []config.AggregatorConfig{
			{
				Stream: "nested_flow_data",
				Window: config.AggregatorWindowConfig{
					IdleFlushAfter: 5000,
				},
				Periodic: config.AggregatorPeriodicConfig{
					Every: 1000,
				},
				KeyFields: []string{
					"outer_src_addr",
					"outer_dst_addr",
					"src_addr",
					"dst_addr",
					"proto",
					"src_port",
					"dst_port",
				},
				TemplateID: 300,
				StaticFields: map[string]any{
					"example": "nested-ip-layers",
				},
				Sum: []string{
					"bytes",
					"packets",
				},
				First: []string{
					"agent_ip",
					"start_time_unix",
				},
				Current: []string{
					"agent_ip",
					"input_if",
					"output_if",
					"sampling_rate",
					"end_time_unix",
				},
			},
		},
	}
}

const nestedIPLayersJSON = `{
  "agent_ip": "192.0.2.10",
  "source_id": 1,
  "sampling_rate": 100,
  "input_if": 10,
  "output_if": 20,
  "packet": {
    "layers": [
      {
        "kind": "ipv4",
        "ipv4": {
          "src_addr": "203.0.113.1",
          "dst_addr": "203.0.113.2",
          "protocol": 47,
          "ttl": 64
        }
      },
      {
        "kind": "gre",
        "gre": {
          "protocol": 2048
        }
      },
      {
        "kind": "ipv4",
        "ipv4": {
          "src_addr": "192.0.2.1",
          "dst_addr": "198.51.100.2",
          "protocol": 6,
          "ttl": 64
        }
      },
      {
        "kind": "tcp",
        "tcp": {
          "src_port": 12345,
          "dst_port": 443,
          "flags": 2,
          "window": 65535
        }
      }
    ]
  },
  "bytes": 96,
  "packets": 1,
  "start_time_unix": 1714483200000,
  "end_time_unix": 1714483200100
}`

const nestedDot1QMPLSGREJSON = `{
  "agent_ip": "192.0.2.10",
  "source_id": 1,
  "sampling_rate": 100,
  "input_if": 10,
  "output_if": 20,
  "packet": {
    "layers": [
      {
        "kind": "ethernet",
        "ethernet": {
          "src_mac": "66:77:88:99:aa:bb",
          "dst_mac": "00:11:22:33:44:55"
        }
      },
      {
        "kind": "dot1q",
        "vlan": {
          "id": 100,
          "tpid": 33024
        }
      },
      {
        "kind": "mpls",
        "mpls": {
          "label": {
            "label": 17,
            "bos": true,
            "ttl": 64
          }
        }
      },
      {
        "kind": "ipv4",
        "ipv4": {
          "src_addr": "203.0.113.1",
          "dst_addr": "203.0.113.2",
          "protocol": 47,
          "ttl": 64
        }
      },
      {
        "kind": "gre",
        "gre": {
          "protocol": 2048
        }
      },
      {
        "kind": "ipv4",
        "ipv4": {
          "src_addr": "192.0.2.1",
          "dst_addr": "198.51.100.2",
          "protocol": 6,
          "ttl": 64
        }
      },
      {
        "kind": "tcp",
        "tcp": {
          "src_port": 12345,
          "dst_port": 443,
          "flags": 2,
          "window": 65535
        }
      }
    ]
  },
  "bytes": 128,
  "packets": 1,
  "start_time_unix": 1714483201000,
  "end_time_unix": 1714483201100
}`

func packetLayerKinds(model *event.PacketModel) []string {
	if model == nil {
		return nil
	}
	layers := make([]string, 0, len(model.Layers))
	for _, layer := range model.Layers {
		layers = append(layers, layer.Kind)
	}
	return layers
}

func TestStatefulInitEventsCarryConfiguredStreamAndTemplateBaseID(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Stream: "agg_packets",
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		Match:      map[string]string{"ip_family": "ipv4"},
		TemplateID: 512,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	events, err := agg.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 init event, got %d", len(events))
	}
	if events[0].Stream != "agg_packets" {
		t.Fatalf("expected event stream agg_packets, got %q", events[0].Stream)
	}
	if events[0].Control == nil || events[0].Control.Stream != "agg_packets" {
		t.Fatalf("expected control stream agg_packets, got %#v", events[0].Control)
	}
	schema, ok := events[0].Payload.(event.AggregationSchema)
	if !ok {
		t.Fatalf("expected aggregation schema payload, got %T", events[0].Payload)
	}
	if schema.Stream != "agg_packets" {
		t.Fatalf("expected schema stream agg_packets, got %q", schema.Stream)
	}
	if schema.BaseTemplateID != 512 {
		t.Fatalf("expected base template id 512, got %d", schema.BaseTemplateID)
	}
	if schema.Match["ip_family"] != "ipv4" {
		t.Fatalf("expected schema match ip_family=ipv4, got %#v", schema.Match)
	}
}

func TestSchemaPassthroughEmitsSchemaAndForwardsEvents(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Passthrough:      true,
		Stream:           "flow_data",
		TemplateID:       256,
		FieldsConfigured: true,
		Fields: []config.AggregatorField{
			{Role: "key", Name: "src_addr"},
			{Role: "current", Name: "bytes"},
			{Role: "static", Name: "exporter_name", Value: "edge-a"},
		},
		KeyFields: []string{"src_addr"},
		StaticFields: map[string]any{
			"exporter_name": "edge-a",
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	initEvents, err := agg.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	if len(initEvents) != 1 {
		t.Fatalf("expected 1 init event, got %d", len(initEvents))
	}
	schema, ok := initEvents[0].Payload.(event.AggregationSchema)
	if !ok {
		t.Fatalf("expected aggregation schema payload, got %T", initEvents[0].Payload)
	}
	if len(schema.Fields) != 3 {
		t.Fatalf("expected 3 schema fields, got %#v", schema.Fields)
	}
	if schema.Fields[0].Name != "src_addr" {
		t.Fatalf("unexpected first schema field: %#v", schema.Fields[0])
	}

	evt := &event.Event{
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"bytes":    uint64(64),
		},
	}
	out, err := agg.Process(evt)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 forwarded event, got %d", len(out))
	}
	if out[0].Fields["exporter_name"] != "edge-a" {
		t.Fatalf("expected static field to be injected, got %#v", out[0].Fields)
	}
	if _, ok := evt.Fields["exporter_name"]; ok {
		t.Fatalf("did not expect original event fields to be mutated")
	}
}

func TestSchemaPassthroughCanEmitControlAsData(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Passthrough:      true,
		Stream:           "options_data",
		EmitAs:           "data",
		FieldsConfigured: true,
		Fields: []config.AggregatorField{
			{Role: "static", Name: "tflow_record_type", Value: "options"},
			{Role: "key", Name: "source_id"},
			{Role: "current", Name: "sampling_rate"},
		},
		KeyFields: []string{"source_id"},
		StaticFields: map[string]any{
			"tflow_record_type": "options",
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	out, err := agg.Process(&event.Event{
		Kind: "control",
		Control: &event.ControlMetadata{
			Type: "source_init",
		},
		Fields: map[string]any{
			"source_id":     uint32(7),
			"sampling_rate": uint32(100),
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 forwarded event, got %d", len(out))
	}
	if out[0].Kind != "data" || out[0].Control != nil {
		t.Fatalf("expected control event to be emitted as data, got kind=%q control=%#v", out[0].Kind, out[0].Control)
	}
	if out[0].Stream != "options_data" {
		t.Fatalf("expected options_data stream, got %q", out[0].Stream)
	}
	if out[0].Fields["tflow_record_type"] != "options" {
		t.Fatalf("expected static options marker, got %#v", out[0].Fields)
	}
}

func TestSchemaFieldsDeduplicateConfiguredNames(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Passthrough:      true,
		Stream:           "flow_data",
		FieldsConfigured: true,
		Fields: []config.AggregatorField{
			{Role: "first", Name: "agent_ip"},
			{Role: "current", Name: "agent_ip"},
			{Role: "sum", Name: "bytes"},
		},
		First:   []string{"agent_ip"},
		Current: []string{"agent_ip"},
		Sum:     []string{"bytes"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	initEvents, err := agg.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	schema, ok := initEvents[0].Payload.(event.AggregationSchema)
	if !ok {
		t.Fatalf("expected aggregation schema payload, got %T", initEvents[0].Payload)
	}
	if len(schema.Fields) != 2 {
		t.Fatalf("expected deduplicated schema fields, got %#v", schema.Fields)
	}
	if schema.Fields[0].Name != "agent_ip" || schema.Fields[1].Name != "bytes" {
		t.Fatalf("unexpected schema field order: %#v", schema.Fields)
	}
}

func TestStatefulInitEventsSortStaticFieldsDeterministically(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Stream: "agg_packets",
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr"},
		Sum:       []string{"bytes"},
		StaticFields: map[string]any{
			"z_field": "z",
			"a_field": "a",
			"m_field": "m",
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	events, err := agg.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	schema, ok := events[0].Payload.(event.AggregationSchema)
	if !ok {
		t.Fatalf("expected aggregation schema payload, got %T", events[0].Payload)
	}
	want := []string{"src_addr", "bytes", "start_time_unix", "end_time_unix", "a_field", "m_field", "z_field"}
	if len(schema.FieldNames) != len(want) {
		t.Fatalf("expected %d field names, got %#v", len(want), schema.FieldNames)
	}
	for i, field := range want {
		if schema.FieldNames[i] != field {
			t.Fatalf("expected field_names[%d]=%q, got %#v", i, field, schema.FieldNames)
		}
	}
}

func TestStatefulAggregatedEventsCarryConfiguredStream(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Stream: "agg_counters",
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if _, err := agg.Process(&event.Event{
		Fields: map[string]any{
			"if_in_octets": int64(64),
		},
	}); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if out[0].Stream != "agg_counters" {
		t.Fatalf("expected stream agg_counters, got %q", out[0].Stream)
	}
}

func TestStatefulStoresSyntheticMetadataInAggregationEnvelope(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr"},
		Sum:       []string{"bytes"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	if _, err := agg.Process(&event.Event{
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"bytes":    int64(64),
		},
	}); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}

	metadata := out[0].Aggregation
	if metadata == nil {
		t.Fatalf("expected aggregation metadata")
	}
	if metadata.Key != "192.0.2.1" {
		t.Fatalf("expected aggregation key 192.0.2.1, got %q", metadata.Key)
	}
	if metadata.FirstSeenUnix == 0 {
		t.Fatalf("expected first_seen_unix to be set")
	}
	if metadata.LastSeenUnix == 0 {
		t.Fatalf("expected last_seen_unix to be set")
	}
	for _, key := range []string{"_aggregation_key", "_first_seen_unix", "_last_seen_unix", "aggregation_key", "first_seen_unix", "last_seen_unix"} {
		if _, ok := out[0].Fields[key]; ok {
			t.Fatalf("did not expect synthetic metadata field %s", key)
		}
	}
}

func TestStatefulTTLFlushSumsPacketCounters(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Window: config.AggregatorWindowConfig{
			IdleFlushAfter: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	first := &event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(60),
			"packets":  int64(1),
		},
	}
	second := &event.Event{
		ReceivedAt: time.Unix(2, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(70),
			"packets":  int64(1),
		},
	}

	if out, err := agg.Process(first); err != nil || len(out) != 0 {
		t.Fatalf("first Process returned out=%d err=%v", len(out), err)
	}
	if out, err := agg.Process(second); err != nil || len(out) != 0 {
		t.Fatalf("second Process returned out=%d err=%v", len(out), err)
	}

	time.Sleep(10 * time.Millisecond)

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if got := out[0].Fields["packets"]; got != int64(2) {
		t.Fatalf("expected packets=2, got %#v", got)
	}
	if got := out[0].Fields["bytes"]; got != int64(130) {
		t.Fatalf("expected bytes=130, got %#v", got)
	}
}

func TestStatefulTracksMinStartAndMaxEndTimestamps(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(10, 0),
		Fields: map[string]any{
			"src_addr":        "192.0.2.1",
			"dst_addr":        "198.51.100.2",
			"proto":           uint32(6),
			"src_port":        uint32(12345),
			"dst_port":        uint32(443),
			"bytes":           int64(60),
			"packets":         int64(1),
			"start_time_unix": int64(5_000),
			"end_time_unix":   int64(8_000),
		},
	})
	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(11, 0),
		Fields: map[string]any{
			"src_addr":        "192.0.2.1",
			"dst_addr":        "198.51.100.2",
			"proto":           uint32(6),
			"src_port":        uint32(12345),
			"dst_port":        uint32(443),
			"bytes":           int64(70),
			"packets":         int64(1),
			"start_time_unix": int64(4_000),
			"end_time_unix":   int64(9_000),
		},
	})

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := out[0].Fields["start_time_unix"]; got != int64(4_000) {
		t.Fatalf("expected start_time_unix=4000, got %#v", got)
	}
	if got := out[0].Fields["end_time_unix"]; got != int64(9_000) {
		t.Fatalf("expected end_time_unix=9000, got %#v", got)
	}
}

func TestStatefulBitwiseAndsConfiguredFields(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		And:       []string{"tcp_flags"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	initEvents, err := agg.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	schema, ok := initEvents[0].Payload.(event.AggregationSchema)
	if !ok {
		t.Fatalf("expected aggregation schema payload, got %T", initEvents[0].Payload)
	}
	if len(schema.AndFields) != 1 || schema.AndFields[0] != "tcp_flags" {
		t.Fatalf("expected schema and_fields tcp_flags, got %#v", schema.AndFields)
	}

	for _, flags := range []uint32{0x13, 0x12, 0x16} {
		_, err := agg.Process(&event.Event{
			Fields: map[string]any{
				"src_addr":  "192.0.2.1",
				"dst_addr":  "198.51.100.2",
				"proto":     uint32(6),
				"src_port":  uint32(12345),
				"dst_port":  uint32(443),
				"tcp_flags": flags,
			},
		})
		if err != nil {
			t.Fatalf("Process returned error: %v", err)
		}
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if got := out[0].Fields["tcp_flags"]; got != uint32(0x12) {
		t.Fatalf("expected tcp_flags=0x12, got %#v", got)
	}
}

func TestStatefulOnlySumsConfiguredSumFields(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(60),
			"packets":  int64(1),
		},
	})
	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(2, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(70),
			"packets":  int64(1),
		},
	})

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if got := out[0].Fields["proto"]; got != uint32(6) {
		t.Fatalf("expected proto=6 to remain stable, got %#v", got)
	}
	if got := out[0].Fields["bytes"]; got != int64(130) {
		t.Fatalf("expected bytes=130, got %#v", got)
	}
	if got := out[0].Fields["packets"]; got != int64(2) {
		t.Fatalf("expected packets=2, got %#v", got)
	}
}

func TestStatefulReadsSourceMetadataFields(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"source_id"},
		Current:   []string{"agent_ip", "sampling_rate", "sample_pool", "drops"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = agg.Process(&event.Event{
		Source: event.SourceMetadata{
			AgentIP:  "198.51.100.99",
			SourceID: 42,
			Sampling: &event.SamplingMetadata{
				Rate:       250,
				SamplePool: 54321,
				Drops:      7,
			},
		},
		Fields: map[string]any{
			"bytes":         uint64(64),
			"agent_ip":      "192.0.2.1",
			"source_id":     uint32(9),
			"sampling_rate": uint32(100),
			"sample_pool":   uint32(12345),
			"drops":         uint32(3),
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected one aggregate, got %d", len(out))
	}
	fields := out[0].Fields
	if fields["agent_ip"] != "198.51.100.99" {
		t.Fatalf("expected agent_ip from metadata, got %#v", fields["agent_ip"])
	}
	if fields["source_id"] != uint32(42) {
		t.Fatalf("expected source_id from metadata, got %#v", fields["source_id"])
	}
	if fields["sampling_rate"] != uint32(250) || fields["sample_pool"] != uint32(54321) || fields["drops"] != uint32(7) {
		t.Fatalf("expected sampling metadata fields, got %#v", fields)
	}
}

func TestStatefulReadsZeroSourceIDWhenSet(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"source_id"},
		Current:   []string{"source_id"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = agg.Process(&event.Event{
		Source: event.SourceMetadata{
			SourceID:    0,
			SourceIDSet: true,
		},
		Fields: map[string]any{
			"bytes": uint64(64),
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected one aggregate, got %d", len(out))
	}
	if got := out[0].Fields["source_id"]; got != uint32(0) {
		t.Fatalf("expected source_id=0 from metadata, got %#v", got)
	}
}

func TestStatefulSplitsAgentIPMetadataByFamily(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		Current: []string{"agent_ip", "agent_ipv6"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = agg.Process(&event.Event{
		Source: event.SourceMetadata{
			AgentIP: "2001:db8::99",
		},
		Fields: map[string]any{
			"bytes":    uint64(64),
			"agent_ip": "192.0.2.1",
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected one aggregate, got %d", len(out))
	}
	fields := out[0].Fields
	if _, ok := fields["agent_ip"]; ok {
		t.Fatalf("expected IPv6 metadata not to populate agent_ip, got %#v", fields)
	}
	if fields["agent_ipv6"] != "2001:db8::99" {
		t.Fatalf("expected agent_ipv6 from metadata, got %#v", fields["agent_ipv6"])
	}
}

func TestStatefulPeriodicFlushOnlyEmitsDirtyBuckets(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every: 1,
		},
		KeyFields: []string{"src_addr", "dst_addr", "proto", "src_port", "dst_port"},
		Sum:       []string{"bytes", "packets"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(60),
			"packets":  int64(1),
		},
	})

	time.Sleep(10 * time.Millisecond)

	firstFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("first Flush returned error: %v", err)
	}
	if len(firstFlush) != 1 {
		t.Fatalf("expected first flush to emit 1 event, got %d", len(firstFlush))
	}

	secondFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("second Flush returned error: %v", err)
	}
	if len(secondFlush) != 0 {
		t.Fatalf("expected second flush to emit 0 events, got %d", len(secondFlush))
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(2, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"dst_addr": "198.51.100.2",
			"proto":    uint32(6),
			"src_port": uint32(12345),
			"dst_port": uint32(443),
			"bytes":    int64(40),
			"packets":  int64(1),
		},
	})

	time.Sleep(10 * time.Millisecond)

	thirdFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("third Flush returned error: %v", err)
	}
	if len(thirdFlush) != 1 {
		t.Fatalf("expected third flush to emit 1 event, got %d", len(thirdFlush))
	}
	if got := thirdFlush[0].Fields["bytes"]; got != int64(100) {
		t.Fatalf("expected bytes=100 after update, got %#v", got)
	}
}

func TestStatefulIdleEraseDropsUntouchedBucketWithoutEmit(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Window: config.AggregatorWindowConfig{
			IdleEraseAfter: 1,
			MaxFlushAfter:  1000,
		},
		KeyFields: []string{"src_addr"},
		Sum:       []string{"bytes"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"bytes":    int64(60),
		},
	})

	time.Sleep(10 * time.Millisecond)

	out, err := agg.Flush()
	if err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected idle erase to drop bucket without emit, got %d events", len(out))
	}
}

func TestStatefulPeriodicResetDeletesBucketsAfterEmit(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Periodic: config.AggregatorPeriodicConfig{
			Every:        1,
			ResetBuckets: true,
		},
		KeyFields: []string{"src_addr"},
		Sum:       []string{"bytes"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, _ = agg.Process(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"src_addr": "192.0.2.1",
			"bytes":    int64(60),
		},
	})

	time.Sleep(10 * time.Millisecond)

	firstFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("first Flush returned error: %v", err)
	}
	if len(firstFlush) != 1 {
		t.Fatalf("expected first flush to emit 1 event, got %d", len(firstFlush))
	}

	secondFlush, err := agg.Flush()
	if err != nil {
		t.Fatalf("second Flush returned error: %v", err)
	}
	if len(secondFlush) != 0 {
		t.Fatalf("expected second flush to emit 0 events after reset, got %d", len(secondFlush))
	}
}
