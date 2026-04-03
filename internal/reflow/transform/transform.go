package transform

import (
	"fmt"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type Transform interface {
	Apply(evt *event.Event) ([]*event.Event, error)
}

func BuildChain(cfgs []config.TransformConfig) ([]Transform, error) {
	transforms := make([]Transform, 0, len(cfgs))
	for _, cfg := range cfgs {
		switch cfg.Type {
		case "", "add_fields":
			transforms = append(transforms, AddFields{Fields: cfg.Fields})
		default:
			return nil, fmt.Errorf("unsupported transform.type %q", cfg.Type)
		}
	}
	return transforms, nil
}

func ApplyChain(chain []Transform, evt *event.Event) ([]*event.Event, error) {
	events := []*event.Event{evt}
	for _, tr := range chain {
		next := make([]*event.Event, 0, len(events))
		for _, item := range events {
			out, err := tr.Apply(item)
			if err != nil {
				return nil, err
			}
			next = append(next, out...)
		}
		events = next
	}
	return events, nil
}

type AddFields struct {
	Fields map[string]any
}

func (t AddFields) Apply(evt *event.Event) ([]*event.Event, error) {
	if len(t.Fields) == 0 {
		return []*event.Event{evt}, nil
	}
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, len(t.Fields))
	}
	for k, v := range t.Fields {
		evt.Fields[k] = v
	}
	return []*event.Event{evt}, nil
}
