package decode

import (
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func routerKey(evt *event.Event) string {
	if evt.Source.Remote != "" {
		return evt.Source.Remote
	}
	return evt.Source.Address
}

func cloneEvent(evt *event.Event) *event.Event {
	item := &event.Event{
		ReceivedAt: evt.ReceivedAt,
		Source:     evt.Source,
		Message:    evt.Message,
		Payload:    evt.Payload,
	}
	if evt.Fields != nil {
		item.Fields = make(map[string]any, len(evt.Fields))
		for k, v := range evt.Fields {
			item.Fields[k] = v
		}
	}
	return item
}

func ensureFields(evt *event.Event, capacity int) map[string]any {
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, capacity)
	}
	return evt.Fields
}

func decodeUint32(val any) uint32 {
	switch v := val.(type) {
	case []byte:
		return uint32(decodeUint64(v))
	case uint32:
		return v
	case uint64:
		return uint32(v)
	default:
		return 0
	}
}

func decodeUint64(val any) uint64 {
	switch v := val.(type) {
	case []byte:
		switch len(v) {
		case 1:
			return uint64(v[0])
		case 2:
			return uint64(binary.BigEndian.Uint16(v))
		case 4:
			return uint64(binary.BigEndian.Uint32(v))
		case 8:
			return binary.BigEndian.Uint64(v)
		default:
			var out uint64
			for _, b := range v {
				out = (out << 8) | uint64(b)
			}
			return out
		}
	case uint64:
		return v
	case uint32:
		return uint64(v)
	default:
		return 0
	}
}

func decodeIPString(val any) string {
	raw, ok := val.([]byte)
	if !ok {
		return fmt.Sprint(val)
	}
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return ""
	}
	return addr.String()
}

func flowTimeFromV5(unixSecs, unixNSecs, sysUptime, switched uint32) int64 {
	exportMs := int64(unixSecs)*1000 + int64(unixNSecs)/1_000_000
	uptimeMs := int64(sysUptime)
	return exportMs - (uptimeMs - int64(switched))
}

func flowTimeFromV9(sysUptime, unixSeconds, switched uint32) int64 {
	exportMs := int64(unixSeconds) * 1000
	uptimeMs := int64(sysUptime)
	return exportMs - (uptimeMs - int64(switched))
}
