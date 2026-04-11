package processor

import (
	"testing"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func TestBuiltinProcessFlowDropsPayload(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{
			DropMessage: true,
			DropPayload: true,
		},
	})

	evt := &event.Event{
		Source:  event.SourceMetadata{Type: "flow"},
		Message: []byte(`{"ignored":true}`),
		Payload: []byte{1, 2, 3},
		Fields: map[string]any{
			"flow_type": "sflow",
		},
	}

	events, err := proc.Process(evt)
	if err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if got := events[0].Fields["message_type"]; got != "flow" {
		t.Fatalf("expected message_type=flow, got %#v", got)
	}
	if events[0].Message != nil {
		t.Fatalf("expected message to be dropped")
	}
	if events[0].Payload != nil {
		t.Fatalf("expected payload to be dropped")
	}
}

func TestBuiltinProcessFlowRequiresFlowType(t *testing.T) {
	proc := NewBuiltin(config.ProcessorConfig{
		Builtin: config.BuiltinProcessorConfig{},
	})

	_, err := proc.Process(&event.Event{
		Source: event.SourceMetadata{Type: "flow"},
	})
	if err == nil {
		t.Fatalf("expected error for missing flow_type")
	}
}
