package decode

import (
	"encoding/binary"
	"reflect"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	flowpb "github.com/netsampler/goflow2/v3/pb"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/encode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"google.golang.org/protobuf/proto"
)

func TestSearchNetFlowOptionDataSetsFindsSamplingRate(t *testing.T) {
	rate, found, err := searchNetFlowOptionDataSets([]netflow.OptionsDataFlowSet{
		{
			Records: []netflow.OptionsDataRecord{
				{
					OptionsValues: []netflow.DataField{
						{Type: 34, Value: []byte{0x00, 0x00, 0x03, 0xe8}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("searchNetFlowOptionDataSets returned error: %v", err)
	}
	if !found {
		t.Fatalf("expected sampling rate to be found")
	}
	if rate != 1000 {
		t.Fatalf("expected sampling rate 1000, got %d", rate)
	}
}

func TestTemplateAndOptionsEventsAreEmitted(t *testing.T) {
	d := &builtIn{}
	base := &event.Event{
		ReceivedAt: time.Unix(1, 0),
		Source:     event.SourceMetadata{Type: "flow"},
	}
	packet := &netflow.IPFIXPacket{Version: 10}

	templateEvents := d.templateEventsFromIPFIX(base, packet,
		[]netflow.TemplateFlowSet{
			{
				Records: []netflow.TemplateRecord{
					{TemplateId: 256, FieldCount: 3, Fields: []netflow.Field{{Type: 1, Length: 8}}},
				},
			},
		},
		[]netflow.IPFIXOptionsTemplateFlowSet{
			{
				Records: []netflow.IPFIXOptionsTemplateRecord{
					{TemplateId: 300, FieldCount: 2, ScopeFieldCount: 1},
				},
			},
		},
	)
	if len(templateEvents) != 2 {
		t.Fatalf("expected 2 template events, got %d", len(templateEvents))
	}
	if templateEvents[0].Fields["flow_type"] != "ipfix_template" {
		t.Fatalf("expected first template event flow_type=ipfix_template, got %#v", templateEvents[0].Fields["flow_type"])
	}
	if templateEvents[1].Fields["flow_type"] != "ipfix_options_template" {
		t.Fatalf("expected second template event flow_type=ipfix_options_template, got %#v", templateEvents[1].Fields["flow_type"])
	}
	if got := templateEvents[1].Fields["tflow_record_type"]; got != "options" {
		t.Fatalf("expected options template tflow_record_type=options, got %#v", got)
	}
	if _, ok := templateEvents[0].Fields["tflow.fields"]; ok {
		t.Fatalf("expected compact template event by default, got %#v", templateEvents[0].Fields["tflow.fields"])
	}

	optionsEvents := d.optionsEventsFromIPFIX(base, packet, []netflow.OptionsDataFlowSet{
		{
			Records: []netflow.OptionsDataRecord{
				{
					OptionsValues: []netflow.DataField{
						{Type: 34, Value: []byte{0x00, 0x00, 0x00, 0x64}},
					},
				},
			},
		},
	})
	if len(optionsEvents) != 1 {
		t.Fatalf("expected 1 options event, got %d", len(optionsEvents))
	}
	if optionsEvents[0].Fields["record_kind"] != "options_data" {
		t.Fatalf("expected record_kind=options_data, got %#v", optionsEvents[0].Fields["record_kind"])
	}
	if got := optionsEvents[0].Fields["tflow_record_type"]; got != "options" {
		t.Fatalf("expected options data tflow_record_type=options, got %#v", got)
	}
	if got := optionsEvents[0].Fields["sampling_rate"]; got != uint32(100) {
		t.Fatalf("expected sampling_rate=100, got %#v", got)
	}
}

func TestTemplateEventsEmbedFieldsWhenConfigured(t *testing.T) {
	d := &builtIn{
		embedTemplateFields: true,
		catalog: newDecodeCatalog(map[string]config.IPFIXFieldDefinition{
			"bytes": {
				ID:     1,
				Length: 8,
				Type:   "unsigned64",
				Format: "delta",
			},
			"custom_counter": {
				Name:             "customCounter",
				ID:               4000,
				PEN:              32473,
				Length:           4,
				Type:             "unsigned32",
				EnterpriseScoped: true,
			},
			"input_if": {
				ID:     10,
				Length: 4,
				Type:   "unsigned32",
			},
			"sampling_rate": {
				ID:     34,
				Length: 4,
				Type:   "unsigned32",
			},
		}),
	}
	base := &event.Event{
		ReceivedAt: time.Unix(1, 0),
		Source:     event.SourceMetadata{Type: "flow"},
	}
	packet := &netflow.IPFIXPacket{Version: 10}

	templateEvents := d.templateEventsFromIPFIX(base, packet,
		[]netflow.TemplateFlowSet{
			{
				Records: []netflow.TemplateRecord{
					{
						TemplateId: 256,
						FieldCount: 3,
						Fields: []netflow.Field{
							{Type: 1, Length: 8},
							{PenProvided: true, Type: 4000, Length: 4, Pen: 32473},
							{Type: 999, Length: 2},
						},
					},
				},
			},
		},
		[]netflow.IPFIXOptionsTemplateFlowSet{
			{
				Records: []netflow.IPFIXOptionsTemplateRecord{
					{
						TemplateId:      300,
						FieldCount:      2,
						ScopeFieldCount: 1,
						Scopes:          []netflow.Field{{Type: 10, Length: 4}},
						Options:         []netflow.Field{{Type: 34, Length: 4}},
					},
				},
			},
		},
	)
	if len(templateEvents) != 2 {
		t.Fatalf("expected 2 template events, got %d", len(templateEvents))
	}

	fields, ok := templateEvents[0].Fields["tflow.fields"].([]map[string]any)
	if !ok {
		t.Fatalf("expected tflow.fields, got %#v", templateEvents[0].Fields["tflow.fields"])
	}
	if len(fields) != 3 {
		t.Fatalf("expected 3 embedded fields, got %#v", fields)
	}
	if fields[0]["name"] != "bytes" || fields[0]["data_type"] != "unsigned64" || fields[0]["format"] != "delta" {
		t.Fatalf("expected catalog-enriched bytes field, got %#v", fields[0])
	}
	if fields[1]["name"] != "customCounter" || fields[1]["key"] != "custom_counter" || fields[1]["pen"] != uint32(32473) {
		t.Fatalf("expected enterprise catalog field, got %#v", fields[1])
	}
	if _, ok := fields[2]["name"]; ok {
		t.Fatalf("expected unknown field to stay raw, got %#v", fields[2])
	}

	scopes, ok := templateEvents[1].Fields["tflow.scopes"].([]map[string]any)
	if !ok || len(scopes) != 1 || scopes[0]["name"] != "input_if" {
		t.Fatalf("expected embedded scope field, got %#v", templateEvents[1].Fields["tflow.scopes"])
	}
	options, ok := templateEvents[1].Fields["tflow.options"].([]map[string]any)
	if !ok || len(options) != 1 || options[0]["name"] != "sampling_rate" {
		t.Fatalf("expected embedded option field, got %#v", templateEvents[1].Fields["tflow.options"])
	}
}

func TestDecodeNetFlowV5EmitsPacketMetadataAndCanonicalFields(t *testing.T) {
	nfv5 := encode.NewNFv5Encoder(config.EncoderConfig{Type: "netflowv5"})
	payloads, err := nfv5.Encode(&event.Event{
		ReceivedAt: time.Unix(1700000000, 0).UTC(),
		Fields: map[string]any{
			"src_addr":        "192.0.2.10",
			"dst_addr":        "198.51.100.20",
			"next_hop":        "203.0.113.1",
			"src_port":        uint32(12345),
			"dst_port":        uint32(443),
			"proto":           uint32(6),
			"tcp_flags":       uint32(0x12),
			"tos":             uint32(184),
			"bytes":           int64(321),
			"packets":         int64(7),
			"input_if":        uint32(10),
			"output_if":       uint32(20),
			"src_as":          uint32(64512),
			"dst_as":          uint32(64513),
			"src_mask":        uint32(24),
			"dst_mask":        uint32(25),
			"engine_type":     uint32(1),
			"engine_id":       uint32(2),
			"sampling_rate":   uint32(100),
			"start_time_unix": int64(1699999999000),
			"end_time_unix":   int64(1700000000000),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	dec := New()
	defer dec.Close()
	events, err := dec.Decode(&event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Payload: payloads[0],
	})
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one NetFlow v5 event, got %d", len(events))
	}
	fields := events[0].Fields
	if fields["flow_type"] != "netflowv5" || fields["flow_version"] != uint16(5) || fields["record_kind"] != "packet" {
		t.Fatalf("expected NetFlow v5 packet metadata, got %#v", fields)
	}
	if fields["src_addr"] != "192.0.2.10" || fields["dst_addr"] != "198.51.100.20" || fields["next_hop"] != "203.0.113.1" {
		t.Fatalf("expected dotted IPv4 fields, got src=%#v dst=%#v next_hop=%#v", fields["src_addr"], fields["dst_addr"], fields["next_hop"])
	}
	for key, want := range map[string]any{
		"sampling_rate": uint32(100),
		"engine_type":   uint32(1),
		"engine_id":     uint32(2),
		"tos":           uint32(184),
		"src_as":        uint32(64512),
		"dst_as":        uint32(64513),
		"src_mask":      uint32(24),
		"dst_mask":      uint32(25),
	} {
		if fields[key] != want {
			t.Fatalf("expected %s=%#v, got %#v", key, want, fields[key])
		}
	}

	protobufEnc, err := encode.New(config.EncoderConfig{Type: "protobuf"})
	if err != nil {
		t.Fatalf("New protobuf encoder returned error: %v", err)
	}
	protobufPayloads, err := protobufEnc.Encode(events[0])
	if err != nil {
		t.Fatalf("protobuf Encode returned error: %v", err)
	}
	var msg flowpb.FlowMessage
	if err := proto.Unmarshal(protobufPayloads[0], &msg); err != nil {
		t.Fatalf("unmarshal protobuf flow message: %v", err)
	}
	if msg.Type != flowpb.FlowMessage_NETFLOW_V5 {
		t.Fatalf("expected protobuf type NETFLOW_V5, got %v", msg.Type)
	}
	if msg.Bytes != 321 || msg.Packets != 7 {
		t.Fatalf("expected protobuf counters bytes=321 packets=7, got bytes=%d packets=%d", msg.Bytes, msg.Packets)
	}
}

func TestMapDataFieldsUsesSharedCatalog(t *testing.T) {
	d := &builtIn{catalog: newDecodeCatalog(map[string]config.IPFIXFieldDefinition{
		"src_addr":        {ID: 8, Length: 4, Type: "ipv4Address"},
		"dst_addr":        {ID: 12, Length: 4, Type: "ipv4Address"},
		"proto":           {ID: 4, Length: 1, Type: "unsigned8"},
		"src_port":        {ID: 7, Length: 2, Type: "unsigned16"},
		"dst_port":        {ID: 11, Length: 2, Type: "unsigned16"},
		"bytes":           {ID: 1, Length: 8, Type: "unsigned64"},
		"packets":         {ID: 2, Length: 8, Type: "unsigned64"},
		"start_time_unix": {ID: 152, Length: 8, Type: "unsigned64"},
		"tenant_id": {
			ID:               4000,
			PEN:              32473,
			EnterpriseScoped: true,
			Length:           8,
			Type:             "unsigned64",
		},
	})}
	fields := map[string]any{}
	d.mapDataFields(fields, []netflow.DataField{
		{Type: 27, Value: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
		{Type: 12, Value: []byte{192, 0, 2, 10}},
		{Type: 4, Value: []byte{6}},
		{Type: 7, Value: []byte{0x1f, 0x90}},
		{Type: 11, Value: []byte{0x01, 0xbb}},
		{Type: 1, Value: testUint64(1234, 8)},
		{Type: 2, Value: testUint64(12, 8)},
		{Type: 152, Value: testUint64(1714860000000, 8)},
		{Type: 4000, PenProvided: true, Pen: 32473, Value: testUint64(99, 8)},
	}, 0, 0, false)

	if fields["src_addr"] != "2001:db8::1" {
		t.Fatalf("expected IPv6 src_addr alias to decode, got %#v", fields["src_addr"])
	}
	if fields["dst_addr"] != "192.0.2.10" {
		t.Fatalf("expected dst_addr to decode, got %#v", fields["dst_addr"])
	}
	if fields["proto"] != uint32(6) || fields["proto_name"] != "tcp" {
		t.Fatalf("expected proto and proto_name, got proto=%#v proto_name=%#v", fields["proto"], fields["proto_name"])
	}
	if fields["src_port"] != uint32(8080) || fields["dst_port"] != uint32(443) {
		t.Fatalf("expected ports from catalog, got src=%#v dst=%#v", fields["src_port"], fields["dst_port"])
	}
	if fields["bytes"] != int64(1234) || fields["packets"] != int64(12) {
		t.Fatalf("expected bytes/packets int64 values, got bytes=%#v packets=%#v", fields["bytes"], fields["packets"])
	}
	if fields["start_time_unix"] != int64(1714860000000) {
		t.Fatalf("expected start_time_unix int64 milliseconds, got %#v", fields["start_time_unix"])
	}
	if fields["tenant_id"] != uint64(99) {
		t.Fatalf("expected enterprise custom field to decode, got %#v", fields["tenant_id"])
	}
}

func TestMapDataFieldsUsesNetFlowV9FieldIDs(t *testing.T) {
	d := &builtIn{catalog: newDecodeCatalog(map[string]config.IPFIXFieldDefinition{
		"custom_counter": {ID: 77, Length: 4, Type: "unsigned32"},
		"start_time_unix": {
			ID:     152,
			Length: 8,
			Type:   "unsigned64",
		},
	})}
	fields := map[string]any{}
	d.mapDataFields(fields, []netflow.DataField{
		{Type: 77, Value: []byte{0, 0, 0, 42}},
		{Type: netflow.NFV9_FIELD_FIRST_SWITCHED, Value: []byte{0, 0, 0x03, 0xe8}},
	}, 2000, 100, true)

	if fields["custom_counter"] != uint32(42) {
		t.Fatalf("expected custom_counter from NetFlow v9 field id, got %#v", fields["custom_counter"])
	}
	if fields["start_time_unix"] != int64(99000) {
		t.Fatalf("expected uptime-relative FIRST_SWITCHED conversion, got %#v", fields["start_time_unix"])
	}
}

func TestOptionsEventsUseCatalogForScopesAndOptions(t *testing.T) {
	d := &builtIn{catalog: newDecodeCatalog(map[string]config.IPFIXFieldDefinition{
		"packets":        {ID: 2, Length: 8, Type: "unsigned64"},
		"if_index":       {ID: 10, Length: 4, Type: "unsigned32"},
		"interface_name": {ID: 82, Length: 0xffff, Type: "string"},
	})}
	base := &event.Event{
		ReceivedAt: time.Unix(1, 0),
		Source:     event.SourceMetadata{Type: "flow"},
	}

	ipfixEvents := d.optionsEventsFromIPFIX(base, &netflow.IPFIXPacket{Version: 10}, []netflow.OptionsDataFlowSet{
		{
			FlowSetHeader: netflow.FlowSetHeader{Id: 1300},
			Records: []netflow.OptionsDataRecord{
				{
					ScopesValues:  []netflow.DataField{{Type: 10, Value: []byte{0, 0, 0, 3}}},
					OptionsValues: []netflow.DataField{{Type: 82, Value: []byte("eth0")}},
				},
			},
		},
	})
	if len(ipfixEvents) != 1 {
		t.Fatalf("expected one IPFIX options event, got %d", len(ipfixEvents))
	}
	if ipfixEvents[0].Fields["if_index"] != uint32(3) || ipfixEvents[0].Fields["interface_name"] != "eth0" {
		t.Fatalf("expected IPFIX options catalog fields, got %#v", ipfixEvents[0].Fields)
	}
	if got := ipfixEvents[0].Fields["tflow_record_type"]; got != "options" {
		t.Fatalf("expected IPFIX options tflow_record_type=options, got %#v", got)
	}

	v9Events := d.optionsEventsFromV9(base, &netflow.NFv9Packet{Version: 9}, []netflow.OptionsDataFlowSet{
		{
			FlowSetHeader: netflow.FlowSetHeader{Id: 1300},
			Records: []netflow.OptionsDataRecord{
				{
					ScopesValues:  []netflow.DataField{{Type: 2, Value: []byte{0, 0, 0, 4}}},
					OptionsValues: []netflow.DataField{{Type: 82, Value: []byte("eth1")}},
				},
			},
		},
	})
	if len(v9Events) != 1 {
		t.Fatalf("expected one NetFlow v9 options event, got %d", len(v9Events))
	}
	if v9Events[0].Fields["if_index"] != uint32(4) || v9Events[0].Fields["interface_name"] != "eth1" {
		t.Fatalf("expected NetFlow v9 options catalog fields, got %#v", v9Events[0].Fields)
	}
	if got := v9Events[0].Fields["tflow_record_type"]; got != "options" {
		t.Fatalf("expected NetFlow v9 options tflow_record_type=options, got %#v", got)
	}
	if _, ok := v9Events[0].Fields["packets"]; ok {
		t.Fatalf("expected NetFlow v9 scope id 2 not to decode as packets, got %#v", v9Events[0].Fields)
	}
}

func TestMapDataFieldsPopulatesTCPFlagsWithoutCatalog(t *testing.T) {
	d := &builtIn{}
	fields := map[string]any{}
	d.mapDataFields(fields, []netflow.DataField{
		{Type: netflow.NFV9_FIELD_TCP_FLAGS, Value: []byte{0x12}},
	}, 0, 0, true)

	if fields["tcp_flags"] != uint32(0x12) {
		t.Fatalf("expected tcp_flags to decode from field 6, got %#v", fields["tcp_flags"])
	}
}

func TestMapDataFieldsUsesCatalogAsSourceOfTruth(t *testing.T) {
	d := &builtIn{catalog: newDecodeCatalog(map[string]config.IPFIXFieldDefinition{
		"interface_name": {ID: 82, Type: "string"},
	})}
	fields := map[string]any{}
	d.mapDataFields(fields, []netflow.DataField{
		{Type: netflow.NFV9_FIELD_TCP_FLAGS, Value: []byte{0x12}},
	}, 0, 0, true)

	if _, ok := fields["tcp_flags"]; ok {
		t.Fatalf("expected unmapped tcp_flags to be omitted when catalog is present, got %#v", fields["tcp_flags"])
	}
}

func TestMapDataFieldsDecodesCatalogStringsAndUnknownTypes(t *testing.T) {
	d := &builtIn{catalog: newDecodeCatalog(map[string]config.IPFIXFieldDefinition{
		"interface_name": {ID: netflow.IPFIX_FIELD_interfaceName, Type: "string"},
		"mystery":        {ID: 5000, Type: "frobnitz"},
	})}
	fields := map[string]any{}
	keys := d.mapDataFields(fields, []netflow.DataField{
		{Type: netflow.IPFIX_FIELD_interfaceName, Value: []byte("eth0")},
		{Type: 5000, Value: []byte{1, 2, 3}},
	}, 0, 0, false)

	if fields["interface_name"] != "eth0" {
		t.Fatalf("expected interface_name string decode, got %#v", fields["interface_name"])
	}
	if fields["mystery"] != "AQID" {
		t.Fatalf("expected unknown catalog type as base64, got %#v", fields["mystery"])
	}
	if !reflect.DeepEqual(keys, []string{"interface_name", "mystery"}) {
		t.Fatalf("expected decoded keys, got %#v", keys)
	}
}

func TestOptionsEventsDecodeScopeAndOptionFields(t *testing.T) {
	d := &builtIn{catalog: newDecodeCatalog(map[string]config.IPFIXFieldDefinition{
		"observation_domain_id": {ID: netflow.IPFIX_FIELD_observationDomainId, Type: "unsigned32"},
		"interface_name":        {ID: netflow.IPFIX_FIELD_interfaceName, Type: "string"},
		"sampling_rate":         {ID: netflow.IPFIX_FIELD_samplingInterval, Type: "unsigned32"},
	})}
	base := &event.Event{
		ReceivedAt: time.Unix(1, 0),
		Source:     event.SourceMetadata{Type: "flow"},
	}
	events := d.optionsEventsFromIPFIX(base, &netflow.IPFIXPacket{Version: 10}, []netflow.OptionsDataFlowSet{
		{
			FlowSetHeader: netflow.FlowSetHeader{Id: 1024},
			Records: []netflow.OptionsDataRecord{
				{
					ScopesValues: []netflow.DataField{
						{Type: netflow.IPFIX_FIELD_observationDomainId, Value: []byte{0, 0, 0, 42}},
					},
					OptionsValues: []netflow.DataField{
						{Type: netflow.IPFIX_FIELD_interfaceName, Value: []byte("eth0")},
						{Type: netflow.IPFIX_FIELD_samplingInterval, Value: []byte{0, 0, 0, 100}},
					},
				},
			},
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected one options event, got %d", len(events))
	}
	fields := events[0].Fields
	if fields["observation_domain_id"] != uint32(42) {
		t.Fatalf("expected decoded scope observation_domain_id, got %#v", fields["observation_domain_id"])
	}
	if fields["interface_name"] != "eth0" {
		t.Fatalf("expected decoded option interface_name, got %#v", fields["interface_name"])
	}
	if fields["sampling_rate"] != uint32(100) {
		t.Fatalf("expected decoded sampling_rate, got %#v", fields["sampling_rate"])
	}
	if !reflect.DeepEqual(fields["tflow.scope"], []string{"observation_domain_id"}) {
		t.Fatalf("expected tflow.scope to list scope keys, got %#v", fields["tflow.scope"])
	}
}

func TestIPFIXCustomCatalogFieldRoundTrip(t *testing.T) {
	catalog := map[string]config.IPFIXFieldDefinition{
		"bytes": {ID: 1, Length: 8, Type: "unsigned64"},
		"tenant_id": {
			ID:               4000,
			PEN:              32473,
			EnterpriseScoped: true,
			Length:           8,
			Type:             "unsigned64",
		},
	}
	enc := encode.NewIPFIXEncoder(config.EncoderConfig{
		Type: "ipfix",
		TemplatedFlow: config.TemplatedFlowConfig{
			TemplateBaseID: 256,
			Data: config.TemplatedFlowDataConfig{
				Select:  []string{"bytes", "tenant_id"},
				Catalog: catalog,
			},
		},
	})
	payloads, err := enc.Encode(&event.Event{
		ReceivedAt: time.Unix(1, 0),
		Fields: map[string]any{
			"bytes":     int64(1234),
			"tenant_id": uint64(99),
		},
	})
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}

	dec := NewWithCatalog(catalog)
	defer dec.Close()
	var flowFields map[string]any
	for _, payload := range payloads {
		events, err := dec.Decode(&event.Event{
			Source:  event.SourceMetadata{Type: "flow"},
			Payload: payload,
		})
		if err != nil {
			t.Fatalf("Decode returned error: %v", err)
		}
		for _, item := range events {
			if item.Fields["flow_type"] == "ipfix" {
				flowFields = item.Fields
			}
		}
	}
	if flowFields == nil {
		t.Fatalf("expected decoded IPFIX data event")
	}
	if flowFields["bytes"] != int64(1234) {
		t.Fatalf("expected bytes to survive round trip, got %#v", flowFields["bytes"])
	}
	if flowFields["tenant_id"] != uint64(99) {
		t.Fatalf("expected tenant_id to survive round trip, got %#v", flowFields["tenant_id"])
	}
}

func testUint64(v uint64, size int) []byte {
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, v)
	return raw[8-size:]
}
