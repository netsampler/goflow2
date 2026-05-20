package decode

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/encode"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
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
					{TemplateId: 256, FieldCount: 3},
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
	if got := optionsEvents[0].Fields["sampling_rate"]; got != uint32(100) {
		t.Fatalf("expected sampling_rate=100, got %#v", got)
	}
}

func TestMapDataFieldsUsesSharedCatalog(t *testing.T) {
	d := &builtIn{catalog: newDecodeCatalog(map[string]config.IPFIXFieldDefinition{
		"src_addr":        {ID: 8, Length: 4, Type: "ipv4Address"},
		"dst_addr":        {ID: 12, Length: 4, Type: "ipv4Address"},
		"proto":           {ID: 4, Length: 1, Type: "unsigned8"},
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
