package encode

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflowlegacy"
)

// stringField reads a required string field and reports a type-aware error.
func stringField(fields map[string]any, key string) (string, error) {
	val, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("missing field %q", key)
	}
	s, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("field %q is not a string", key)
	}
	return s, nil
}

// stringFieldOrZero reads an optional string field and returns an empty string when absent.
func stringFieldOrZero(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	val, ok := fields[key]
	if !ok {
		return ""
	}
	s, _ := val.(string)
	return s
}

// uint32Field normalizes common integer representations from the generic field map.
func uint32Field(fields map[string]any, key string) uint32 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case uint32:
		return v
	case uint64:
		return uint32(v)
	case int:
		if v < 0 {
			return 0
		}
		return uint32(v)
	case int64:
		if v < 0 {
			return 0
		}
		return uint32(v)
	case float64:
		if v < 0 {
			return 0
		}
		return uint32(v)
	case json.Number:
		n, _ := v.Int64()
		if n < 0 {
			return 0
		}
		return uint32(n)
	default:
		return 0
	}
}

// uint64Field normalizes common integer representations from the generic field map.
func uint64Field(fields map[string]any, key string) uint64 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	return uint64FromAny(val)
}

// int64Field normalizes common integer representations from the generic field map.
func int64Field(fields map[string]any, key string) int64 {
	if fields == nil {
		return 0
	}
	val, ok := fields[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int64:
		return v
	case uint64:
		return int64(v)
	case int:
		return int64(v)
	case uint32:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	default:
		return 0
	}
}

// bytesField returns a byte-oriented field in either raw []byte or string form.
func bytesField(fields map[string]any, key string) []byte {
	if fields == nil {
		return nil
	}
	val, ok := fields[key]
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	default:
		return nil
	}
}

// encodeIPBytes converts string IPs into the byte-oriented base64 shape used by
// the legacy goflow2v2 JSON payload.
func encodeIPBytes(ip string) string {
	if ip == "" {
		return ""
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(addr.AsSlice())
}

// flowTypeField maps string flow-type labels to the integer enum values expected
// by the legacy goflow2v2 JSON layout.
func flowTypeField(fields map[string]any) any {
	val := stringFieldOrZero(fields, "flow_type")
	switch val {
	case "sflow":
		return 1
	case "netflowv5":
		return 2
	case "netflowv9":
		return 3
	case "ipfix":
		return 4
	case "":
		return 0
	default:
		return val
	}
}

// timeFlowNS prefers the nanosecond timestamp aliases and falls back to the
// millisecond fields that older ReFlow paths still use for aggregation/export.
func timeFlowNS(fields map[string]any, nsKey, msKey string) int64 {
	if ns := int64Field(fields, nsKey); ns > 0 {
		return ns
	}
	if ms := int64Field(fields, msKey); ms > 0 {
		return ms * int64(time.Millisecond)
	}
	return 0
}

// uint16Field is a convenience wrapper around the generic uint32 field reader.
func uint16Field(fields map[string]any, key string) uint16 {
	return uint16(uint32Field(fields, key))
}

// exportUnixMilliseconds picks the best available export timestamp for encoders
// that need an absolute export time even when flow timings are absent.
func exportUnixMilliseconds(receivedAt time.Time, fields map[string]any) int64 {
	endMS := int64Field(fields, "end_time_unix")
	if endMS > 0 {
		return endMS
	}
	startMS := int64Field(fields, "start_time_unix")
	if startMS > 0 {
		return startMS
	}
	if !receivedAt.IsZero() {
		return receivedAt.UnixMilli()
	}
	return time.Now().UnixMilli()
}

// uptimeWindow derives the relative uptime values expected by NetFlow v5 from
// absolute millisecond timestamps.
func uptimeWindow(exportMS, startMS, endMS int64) (sysUptime, first, last uint32) {
	if startMS <= 0 {
		startMS = exportMS
	}
	if endMS <= 0 {
		endMS = exportMS
	}
	baseMS := exportMS
	if startMS < baseMS {
		baseMS = startMS
	}
	if endMS < baseMS {
		baseMS = endMS
	}
	if exportMS < baseMS {
		baseMS = exportMS
	}
	return uint32(exportMS - baseMS), uint32(startMS - baseMS), uint32(endMS - baseMS)
}

// mustIPv4Address parses an IPv4 string field into the legacy NetFlow v5 integer form.
func mustIPv4Address(fields map[string]any, key string) netflowlegacy.IPAddress {
	ip := stringFieldOrZero(fields, key)
	if ip == "" {
		return 0
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return 0
	}
	raw := addr.As4()
	return netflowlegacy.IPAddress(uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3]))
}

// uint64FromAny normalizes several integer representations into uint64 for encoders.
func uint64FromAny(val any) uint64 {
	switch v := val.(type) {
	case uint64:
		return v
	case uint32:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint8:
		return uint64(v)
	case int64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case int:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case float64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case json.Number:
		n, _ := v.Int64()
		if n < 0 {
			return 0
		}
		return uint64(n)
	default:
		return 0
	}
}
