package decode

import (
	"fmt"

	"github.com/netsampler/goflow2/v3/internal/reflow/event"
	"github.com/netsampler/goflow2/v3/utils/store/samplingrate"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

// Decoder identifies and decodes source payloads into the next runtime event shape.
type Decoder interface {
	Decode(evt *event.Event) ([]*event.Event, error)
	Close()
}

// New returns the built-in decoder used by the current runtime.
func New() Decoder {
	store := templates.NewTemplateFlowStore()
	store.Start()
	sampling := samplingrate.NewSamplingRateFlowStore()
	sampling.Start()
	return &builtIn{
		templates: store,
		sampling:  sampling,
	}
}

type builtIn struct {
	templates *templates.TemplateFlowStore
	sampling  samplingrate.Store
}

// Close stops decoder-owned stores and their background sweepers.
func (d *builtIn) Close() {
	if d == nil {
		return
	}
	if d.templates != nil {
		d.templates.Close()
	}
	if d.sampling != nil {
		d.sampling.Close()
	}
}

// Decode handles protocol identification for raw flow payloads and passes through other event types.
func (d *builtIn) Decode(evt *event.Event) ([]*event.Event, error) {
	if evt != nil && evt.Kind == "control" {
		return []*event.Event{evt}, nil
	}
	switch evt.Source.Type {
	case "flow":
		return d.decodeFlow(evt)
	case "bytes":
		return decodeBytes(evt)
	default:
		return []*event.Event{evt}, nil
	}
}

// decodeBytes marks raw packet inputs as bytes payloads and leaves the actual
// tuple extraction to the processor stage.
func decodeBytes(evt *event.Event) ([]*event.Event, error) {
	payload, ok := evt.Payload.([]byte)
	if !ok || len(payload) == 0 {
		return nil, fmt.Errorf("decode bytes: missing payload bytes")
	}
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, 1)
	}
	return []*event.Event{evt}, nil
}
