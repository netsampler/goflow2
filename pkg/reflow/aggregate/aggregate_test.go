package aggregate

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/processor"
)

func TestStatefulMergesConfiguredFields(t *testing.T) {
	for _, initiallyMissing := range []bool{false, true} {
		t.Run(fmt.Sprintf("initially_missing=%t", initiallyMissing), func(t *testing.T) {
			agg := NewStateful(config.AggregatorConfig{
				KeyFields:    []string{"src_addr", "proto"},
				Sum:          []string{"bytes", "packets"},
				First:        []string{"input_if"},
				Current:      []string{"output_if"},
				Min:          []string{"ip_ttl"},
				Max:          []string{"signal_dbm"},
				And:          []string{"tcp_flags"},
				StaticFields: map[string]any{"exporter_name": "edge-a"},
			})
			for i, flags := range []uint32{0x13, 0x12, 0x16} {
				fields := map[string]any{
					"src_addr": "192.0.2.1", "proto": uint32(6),
					"bytes": int64(60 + i*10), "packets": int64(1),
					"input_if": uint32(10 + i), "output_if": uint32(20 + i),
					"ip_ttl": []uint32{64, 60, 62}[i], "signal_dbm": []int64{-50, -45, -48}[i],
					"tcp_flags": flags, "unselected": "ignored",
					"start_time_unix": []int64{5000, 3000, 4000}[i],
					"end_time_unix":   []int64{8000, 10000, 9000}[i],
				}
				if initiallyMissing && i == 0 {
					for _, name := range []string{"input_if", "output_if", "ip_ttl", "signal_dbm", "tcp_flags"} {
						delete(fields, name)
					}
				}
				if out, err := agg.Process(&event.Event{Fields: fields}); err != nil || len(out) != 0 {
					t.Fatalf("Process event %d: out=%v err=%v", i, out, err)
				}
			}
			out, err := agg.Close()
			if err != nil || len(out) != 1 {
				t.Fatalf("Close: out=%v err=%v", out, err)
			}
			firstInput := uint32(10)
			if initiallyMissing {
				firstInput++
			}
			want := map[string]any{
				"src_addr": "192.0.2.1", "proto": uint32(6),
				"bytes": int64(210), "packets": int64(3),
				"input_if": firstInput, "output_if": uint32(22),
				"ip_ttl": uint32(60), "signal_dbm": int64(-45),
				"tcp_flags": uint32(0x12), "exporter_name": "edge-a",
				"start_time_unix": int64(3000), "end_time_unix": int64(10000),
			}
			if !reflect.DeepEqual(out[0].Fields, want) {
				t.Errorf("aggregate fields:\n got %#v\nwant %#v", out[0].Fields, want)
			}
		})
	}
}

func TestStatefulKeepsDistinctKeys(t *testing.T) {
	agg := NewStateful(config.AggregatorConfig{
		AggregateMissing: true,
		KeyFields:        []string{"exporter", "interface_name"},
		Sum:              []string{"bytes"},
	})
	inputs := []map[string]any{
		{"exporter": "edge|a", "interface_name": "uplink", "bytes": int64(10)},
		{"exporter": "edge", "interface_name": "a|uplink", "bytes": int64(20)},
		{"exporter": "edge|a", "interface_name": "uplink", "bytes": int64(30)},
		{"interface_name": "uplink", "bytes": int64(50)},
		{"exporter": "nil", "interface_name": "uplink", "bytes": int64(60)},
		{"exporter": []any{"edge a", "uplink"}, "bytes": int64(70)},
		{"exporter": []any{"edge", "a uplink"}, "bytes": int64(80)},
		{"exporter": map[string]any{"port": "uplink site:edge"}, "bytes": int64(90)},
		{"exporter": map[string]any{"site": "edge", "port": "uplink"}, "bytes": int64(100)},
		{"exporter": uint32(6), "bytes": int64(1)},
		{"exporter": int64(6), "bytes": int64(2)},
		{"exporter": float64(6), "bytes": int64(3)},
		{"exporter": "6", "bytes": int64(110)},
	}
	for _, fields := range inputs {
		if _, err := agg.Process(&event.Event{Fields: fields}); err != nil {
			t.Fatal(err)
		}
	}
	out, err := agg.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Each expected bucket has a unique counter, so structured key values can
	// be compared directly without relying on the internal key encoding.
	got := make(map[int64][2]any)
	for _, evt := range out {
		got[evt.Fields["bytes"].(int64)] = [2]any{evt.Fields["exporter"], evt.Fields["interface_name"]}
	}
	want := map[int64][2]any{
		40:  {"edge|a", "uplink"},
		20:  {"edge", "a|uplink"},
		50:  {nil, "uplink"},
		60:  {"nil", "uplink"},
		70:  {[]any{"edge a", "uplink"}, nil},
		80:  {[]any{"edge", "a uplink"}, nil},
		90:  {map[string]any{"port": "uplink site:edge"}, nil},
		100: {map[string]any{"site": "edge", "port": "uplink"}, nil},
		6:   {float64(6), nil},
		110: {"6", nil},
	}
	if len(out) != len(want) || !reflect.DeepEqual(got, want) {
		t.Errorf("buckets: got %v, want %v", got, want)
	}
}

func TestStatefulReturnsErrorWithMissingKey(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), `missing aggregation key field "input_if"`) {
		t.Fatalf("expected missing key error, got out=%d err=%v", len(out), err)
	}
}

func TestStatefulAggregatesMissingKeyWhenConfigured(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		AggregateMissing: true,
		KeyFields:        []string{"src_addr", "dst_addr"},
		Sum:              []string{"bytes"},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	out, err := agg.Process(&event.Event{
		Fields: map[string]any{
			"dst_addr": "198.51.100.2",
			"bytes":    int64(60),
		},
	})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected aggregation to buffer event, got %d events", len(out))
	}

	out, err = agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 aggregated event, got %d", len(out))
	}
	if _, ok := out[0].Fields["src_addr"]; ok {
		t.Fatalf("did not expect missing src_addr to be added to output fields: %#v", out[0].Fields)
	}
	if got := out[0].Fields["dst_addr"]; got != "198.51.100.2" {
		t.Fatalf("expected dst_addr to be preserved, got %#v", got)
	}
	if got := out[0].Fields["bytes"]; got != int64(60) {
		t.Fatalf("expected bytes=60, got %#v", got)
	}
	if out[0].Aggregation == nil || out[0].Aggregation.Key == "" {
		t.Fatal("expected aggregation metadata with a bucket key")
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
		And:        []string{"tcp_flags"},
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
	if !reflect.DeepEqual(schema.AndFields, []string{"tcp_flags"}) {
		t.Fatalf("expected schema and_fields tcp_flags, got %#v", schema.AndFields)
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

func TestSchemaInitEventsCarryFieldFamilyModifiers(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		Passthrough:      true,
		Stream:           "flow_data",
		FieldsConfigured: true,
		Fields: []config.AggregatorField{
			{Role: "key", Name: "src_addr", Value: "ip6"},
			{Role: "current", Name: "nat_src_addr", Value: "ip4in6"},
			{Role: "sum", Name: "bytes"},
		},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	initEvents, err := agg.InitEvents()
	if err != nil {
		t.Fatalf("InitEvents returned error: %v", err)
	}
	schema := initEvents[0].Payload.(event.AggregationSchema)
	if schema.Fields[0].Value != "ip6" || schema.Fields[1].Value != "ip4in6" {
		t.Fatalf("expected schema values to preserve family modifiers, got %#v", schema.Fields)
	}
}

func TestStatefulAppliesFieldFamilyModifiers(t *testing.T) {
	agg, err := New(config.AggregatorConfig{
		FieldsConfigured: true,
		Fields: []config.AggregatorField{
			{Role: "key", Name: "src_addr", Value: "ip4in6"},
			{Role: "current", Name: "dst_addr", Value: "ip6"},
			{Role: "sum", Name: "bytes"},
		},
		Periodic: config.AggregatorPeriodicConfig{Every: 1},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	_, err = agg.Process(&event.Event{Fields: map[string]any{
		"src_addr": "192.0.2.10",
		"dst_addr": "2001:db8::20",
		"bytes":    uint64(64),
	}})
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	out, err := agg.Close()
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	fields := out[0].Fields
	if fields["src_addr"] != "::ffff:192.0.2.10" {
		t.Fatalf("expected IPv4 source to be mapped into IPv6, got %#v", fields["src_addr"])
	}
	if fields["dst_addr"] != "2001:db8::20" {
		t.Fatalf("expected IPv6 destination to be kept, got %#v", fields["dst_addr"])
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
	if metadata.Key == "" {
		t.Fatal("expected a nonempty aggregation key")
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

func TestStatefulWindowFlushPolicies(t *testing.T) {
	for _, tt := range []struct {
		name       string
		window     config.AggregatorWindowConfig
		periodic   config.AggregatorPeriodicConfig
		due        time.Duration
		wantEvents int
	}{
		{
			name:   "idle window follows last update",
			window: config.AggregatorWindowConfig{IdleFlushAfter: 10},
			due:    15 * time.Millisecond, wantEvents: 1,
		},
		{
			name:   "maximum lifetime survives updates",
			window: config.AggregatorWindowConfig{MaxFlushAfter: 10, IdleFlushAfter: 10},
			due:    10 * time.Millisecond, wantEvents: 1,
		},
		{
			name:   "idle erase removes the bucket",
			window: config.AggregatorWindowConfig{IdleEraseAfter: 10, MaxFlushAfter: 100},
			due:    15 * time.Millisecond,
		},
		{
			name:     "window export wins over periodic export and erase",
			window:   config.AggregatorWindowConfig{MaxFlushAfter: 10, IdleEraseAfter: 5},
			periodic: config.AggregatorPeriodicConfig{Every: 10},
			due:      10 * time.Millisecond, wantEvents: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			agg := NewStateful(config.AggregatorConfig{
				Window: tt.window, Periodic: tt.periodic,
				KeyFields: []string{"src_addr"}, Sum: []string{"bytes"},
			})
			for _, bytes := range []int64{60, 40} {
				if _, err := agg.Process(&event.Event{
					ReceivedAt: time.Unix(1, 0),
					Fields:     map[string]any{"src_addr": "192.0.2.1", "bytes": bytes},
				}); err != nil {
					t.Fatal(err)
				}
			}
			// Drive the existing flush clock directly, without sleeps. The bucket
			// starts at base and receives an update five milliseconds later.
			base := time.Unix(1700000000, 0)
			agg.startedAt = base
			for key, record := range agg.state {
				record.FirstSeen = base
				record.LastSeen = base.Add(5 * time.Millisecond)
				agg.state[key] = record
			}
			if out := agg.flushAt(base.Add(tt.due-time.Millisecond), false); len(out) != 0 || len(agg.state) != 1 {
				t.Fatalf("before deadline: events=%d buckets=%d", len(out), len(agg.state))
			}
			out := agg.flushAt(base.Add(tt.due), false)
			if len(out) != tt.wantEvents || len(agg.state) != 0 {
				t.Fatalf("at deadline: events=%d want=%d buckets=%d", len(out), tt.wantEvents, len(agg.state))
			}
			if len(out) == 1 && out[0].Fields["bytes"] != int64(100) {
				t.Fatalf("exported counter: got %v, want 100", out[0].Fields["bytes"])
			}
			if out, err := agg.Close(); err != nil || len(out) != 0 {
				t.Fatalf("Close after eviction: out=%v err=%v", out, err)
			}
		})
	}
}

func TestStatefulPeriodicSnapshots(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprintf("reset=%t", reset), func(t *testing.T) {
			agg := NewStateful(config.AggregatorConfig{
				Periodic:  config.AggregatorPeriodicConfig{Every: 10, ResetBuckets: reset},
				KeyFields: []string{"src_addr"}, Sum: []string{"bytes"},
			})
			process := func(bytes int64) {
				t.Helper()
				if _, err := agg.Process(&event.Event{
					Fields: map[string]any{"src_addr": "192.0.2.1", "bytes": bytes},
				}); err != nil {
					t.Fatal(err)
				}
			}
			base := agg.startedAt
			process(60)
			if out := agg.flushAt(base.Add(9*time.Millisecond), false); len(out) != 0 {
				t.Fatalf("exported before periodic deadline: %v", out)
			}
			first := agg.flushAt(base.Add(10*time.Millisecond), false)
			if len(first) != 1 || first[0].Fields["bytes"] != int64(60) {
				t.Fatalf("first snapshot: %v", first)
			}
			if out := agg.flushAt(base.Add(20*time.Millisecond), false); len(out) != 0 {
				t.Fatalf("exported unchanged bucket: %v", out)
			}
			process(40)
			second := agg.flushAt(base.Add(30*time.Millisecond), false)
			want := int64(100)
			if reset {
				want = 40
			}
			if len(second) != 1 || second[0].Fields["bytes"] != want {
				t.Fatalf("updated snapshot: got %v, want bytes=%d", second, want)
			}
			if first[0].Fields["bytes"] != int64(60) {
				t.Fatalf("updating bucket mutated previous snapshot: %v", first[0].Fields)
			}
		})
	}
}
