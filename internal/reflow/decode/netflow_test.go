package decode

import (
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
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
