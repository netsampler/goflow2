package decode

import (
	"fmt"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

// Decoder identifies and decodes source payloads into the next runtime event shape.
type Decoder interface {
	Decode(evt *event.Event) ([]*event.Event, error)
}

// New returns the built-in decoder used by the current runtime.
func New() Decoder {
	store := templates.NewTemplateFlowStore()
	store.Start()
	return &builtIn{templates: store}
}

type builtIn struct {
	templates *templates.TemplateFlowStore
}

// Decode handles protocol identification for raw flow payloads and passes through other event types.
func (d *builtIn) Decode(evt *event.Event) ([]*event.Event, error) {
	switch evt.Source.Type {
	case "flow":
		return d.decodeFlow(evt)
	case "bytes":
		return decodeBytes(evt)
	default:
		return []*event.Event{evt}, nil
	}
}

func decodeBytes(evt *event.Event) ([]*event.Event, error) {
	payload, ok := evt.Payload.([]byte)
	if !ok || len(payload) == 0 {
		return nil, fmt.Errorf("decode bytes: missing payload bytes")
	}
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, 1)
	}
	evt.Fields["message_type"] = "bytes"
	return []*event.Event{evt}, nil
}
