package decode

import (
	"encoding/binary"
	"fmt"

	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

// Decoder identifies and decodes source payloads into the next runtime event shape.
type Decoder interface {
	Decode(evt *event.Event) ([]*event.Event, error)
}

// New returns the built-in decoder used by the current runtime.
func New() Decoder {
	return builtIn{}
}

type builtIn struct{}

// Decode handles protocol identification for raw flow payloads and passes through other event types.
func (builtIn) Decode(evt *event.Event) ([]*event.Event, error) {
	switch evt.Source.Type {
	case "flow":
		return decodeFlow(evt)
	case "bytes":
		return decodeBytes(evt)
	default:
		return []*event.Event{evt}, nil
	}
}

func decodeFlow(evt *event.Event) ([]*event.Event, error) {
	payload, ok := evt.Payload.([]byte)
	if !ok || len(payload) == 0 {
		return nil, fmt.Errorf("decode flow: missing payload bytes")
	}
	flowType, flowVersion, err := identifyFlow(payload)
	if err != nil {
		return nil, err
	}
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, 3)
	}
	evt.Fields["message_type"] = "flow"
	evt.Fields["flow_type"] = flowType
	evt.Fields["flow_version"] = flowVersion
	return []*event.Event{evt}, nil
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

func identifyFlow(payload []byte) (string, uint32, error) {
	if len(payload) < 4 {
		return "", 0, fmt.Errorf("identify flow: payload too short")
	}
	if binary.BigEndian.Uint32(payload[:4]) == 5 {
		return "sflow", 5, nil
	}
	switch version := binary.BigEndian.Uint16(payload[:2]); version {
	case 5:
		return "netflowv5", uint32(version), nil
	case 9:
		return "netflowv9", uint32(version), nil
	case 10:
		return "ipfix", uint32(version), nil
	default:
		return "", 0, fmt.Errorf("identify flow: unsupported version %d", version)
	}
}
