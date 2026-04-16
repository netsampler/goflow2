package decode

import (
	"encoding/binary"
	"fmt"

	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func (d *builtIn) decodeFlow(evt *event.Event) ([]*event.Event, error) {
	payload, ok := evt.Payload.([]byte)
	if !ok || len(payload) == 0 {
		return nil, fmt.Errorf("decode flow: missing payload bytes")
	}
	flowType, flowVersion, err := identifyFlow(payload)
	if err != nil {
		return nil, err
	}
	switch flowType {
	case "sflow":
		return d.decodeSFlow(evt, payload, flowVersion)
	case "netflowv5":
		return d.decodeNetFlowV5(evt, payload)
	case "netflowv9":
		return d.decodeNetFlowV9(evt, payload)
	case "ipfix":
		return d.decodeIPFIX(evt, payload)
	default:
		return nil, fmt.Errorf("decode flow: unsupported flow type %q", flowType)
	}
}

// identifyFlow distinguishes sFlow from NetFlow/IPFIX before full decoding.
// sFlow uses a 32-bit leading version field while NetFlow/IPFIX use 16-bit.
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
