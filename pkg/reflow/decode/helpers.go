package decode

import (
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func routerKey(evt *event.Event) string {
	if evt.Source.Remote != "" {
		return evt.Source.Remote
	}
	return evt.Source.Address
}

// cloneEvent preserves source metadata and shared envelope fields while giving
// downstream decode paths an isolated Fields map to mutate.
func cloneEvent(evt *event.Event) *event.Event {
	item := &event.Event{
		ReceivedAt: evt.ReceivedAt,
		Source:     evt.Source,
		Message:    evt.Message,
		Payload:    evt.Payload,
		Packet:     evt.Packet,
	}
	if evt.SFlow != nil {
		copy := *evt.SFlow
		item.SFlow = &copy
	}
	if evt.Fields != nil {
		item.Fields = make(map[string]any, len(evt.Fields))
		for k, v := range evt.Fields {
			item.Fields[k] = v
		}
	}
	return item
}

// ensureFields lazily allocates the mutable field map for an event.
func ensureFields(evt *event.Event, capacity int) map[string]any {
	if evt.Fields == nil {
		evt.Fields = make(map[string]any, capacity)
	}
	return evt.Fields
}

// decodeUint32 normalizes decoder field payloads that may arrive either as raw
// bytes or already-decoded integer types.
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

// fieldUint32 reads one field from a generic field map and normalizes it to u32.
func fieldUint32(fields map[string]any, key string) uint32 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	return decodeUint32(val)
}

// decodeUint64 handles big-endian raw field encodings with varying lengths.
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

// decodeIPString converts raw IP bytes into Go's canonical string form.
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

// ipSliceString is the byte-slice-only variant used by decoder paths that
// already know the source type.
func ipSliceString(raw []byte) string {
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return ""
	}
	return addr.String()
}

// flowTimeFromV5 reconstructs an absolute Unix millisecond timestamp from the
// exporter's uptime-relative NetFlow v5 timestamps.
func flowTimeFromV5(unixSecs, unixNSecs, sysUptime, switched uint32) int64 {
	exportMs := int64(unixSecs)*1000 + int64(unixNSecs)/1_000_000
	uptimeMs := int64(sysUptime)
	return exportMs - (uptimeMs - int64(switched))
}

// flowTimeFromV9 reconstructs an absolute Unix millisecond timestamp from the
// exporter's uptime-relative NetFlow v9 timestamps.
func flowTimeFromV9(sysUptime, unixSeconds, switched uint32) int64 {
	exportMs := int64(unixSeconds) * 1000
	uptimeMs := int64(sysUptime)
	return exportMs - (uptimeMs - int64(switched))
}
