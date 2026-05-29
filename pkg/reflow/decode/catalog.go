package decode

import (
	"fmt"
	"math"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

type decodeCatalog struct {
	ipfix           map[decodeCatalogIPFIXKey]decodeCatalogField
	netflowV9       map[uint16]decodeCatalogField
	netflowV9Scopes map[uint16]decodeCatalogField
}

type decodeCatalogIPFIXKey struct {
	id  uint16
	pen uint32
}

type decodeCatalogField struct {
	key string
	def config.IPFIXFieldDefinition
}

func newDecodeCatalog(catalog map[string]config.IPFIXFieldDefinition) decodeCatalog {
	out := decodeCatalog{
		ipfix:           make(map[decodeCatalogIPFIXKey]decodeCatalogField),
		netflowV9:       make(map[uint16]decodeCatalogField),
		netflowV9Scopes: make(map[uint16]decodeCatalogField),
	}
	for key, def := range catalog {
		field := decodeCatalogField{key: key, def: def}
		for _, ipfixKey := range decodeCatalogIPFIXKeys(key, def) {
			out.ipfix[ipfixKey] = field
		}
		for _, id := range decodeCatalogNetFlowV9IDs(key, def) {
			out.netflowV9[id] = field
		}
		for _, id := range decodeCatalogNetFlowV9ScopeIDs(key, def) {
			out.netflowV9Scopes[id] = field
		}
	}
	return out
}

func (c decodeCatalog) empty() bool {
	return len(c.ipfix) == 0 && len(c.netflowV9) == 0 && len(c.netflowV9Scopes) == 0
}

func (c decodeCatalog) lookup(field netflow.DataField, netflowV9 bool) (decodeCatalogField, bool) {
	if netflowV9 {
		out, ok := c.netflowV9[field.Type]
		return out, ok
	}
	pen := uint32(0)
	if field.PenProvided {
		pen = field.Pen
	}
	out, ok := c.ipfix[decodeCatalogIPFIXKey{id: field.Type, pen: pen}]
	return out, ok
}

func (c decodeCatalog) lookupOptions(field netflow.DataField, netflowV9 bool, scope bool) (decodeCatalogField, bool) {
	if netflowV9 && scope {
		out, ok := c.netflowV9Scopes[field.Type]
		return out, ok
	}
	return c.lookup(field, netflowV9)
}

func decodeCatalogIPFIXKeys(name string, def config.IPFIXFieldDefinition) []decodeCatalogIPFIXKey {
	if def.ID == 0 {
		return nil
	}
	pen := uint32(0)
	if def.EnterpriseScoped || def.PEN != 0 {
		pen = def.PEN
	}
	keys := []decodeCatalogIPFIXKey{{id: def.ID, pen: pen}}
	switch name {
	case "src_addr":
		keys = append(keys, decodeCatalogIPFIXKey{id: 27})
	case "dst_addr":
		keys = append(keys, decodeCatalogIPFIXKey{id: 28})
	}
	return keys
}

func decodeCatalogNetFlowV9IDs(name string, def config.IPFIXFieldDefinition) []uint16 {
	ids := make([]uint16, 0, 2)
	if def.ID != 0 && def.PEN == 0 && !def.EnterpriseScoped {
		ids = append(ids, def.ID)
	}
	switch name {
	case "src_addr":
		ids = append(ids, 27)
	case "dst_addr":
		ids = append(ids, 28)
	case "start_time_unix":
		ids = append(ids, netflow.NFV9_FIELD_FIRST_SWITCHED)
	case "end_time_unix":
		ids = append(ids, netflow.NFV9_FIELD_LAST_SWITCHED)
	}
	return ids
}

func decodeCatalogNetFlowV9ScopeIDs(name string, def config.IPFIXFieldDefinition) []uint16 {
	ids := make([]uint16, 0, 1)
	if id := defaultNetFlowV9ScopeID(name); id != 0 {
		ids = append(ids, id)
	}
	return ids
}

func defaultNetFlowV9ScopeID(name string) uint16 {
	switch name {
	case "observation_domain_id", "source_id":
		return 1
	case "if_index", "input_if":
		return 2
	default:
		return 0
	}
}

func applyCatalogDataField(fields map[string]any, field netflow.DataField, catalogField decodeCatalogField, sysUptime, unixSeconds uint32, netflowV9 bool) {
	key := catalogField.key
	switch key {
	case "proto":
		proto := decodeUint32(field.Value)
		fields["proto"] = proto
		fields["proto_name"] = ipProtocolName(proto)
	case "bytes", "packets":
		fields[key] = int64(decodeUint64(field.Value))
	case "start_time_unix":
		if netflowV9 && field.Type == netflow.NFV9_FIELD_FIRST_SWITCHED {
			fields[key] = flowTimeFromV9(sysUptime, unixSeconds, decodeUint32(field.Value))
		} else {
			fields[key] = int64(decodeUint64(field.Value))
		}
	case "end_time_unix":
		if netflowV9 && field.Type == netflow.NFV9_FIELD_LAST_SWITCHED {
			fields[key] = flowTimeFromV9(sysUptime, unixSeconds, decodeUint32(field.Value))
		} else {
			fields[key] = int64(decodeUint64(field.Value))
		}
	default:
		fields[key] = decodeCatalogValue(catalogField.def, field.Value)
	}
}

func decodeCatalogValue(def config.IPFIXFieldDefinition, val any) any {
	switch def.Type {
	case "ipv4Address", "ipv6Address":
		return decodeIPString(val)
	case "string":
		switch v := val.(type) {
		case []byte:
			return string(v)
		case string:
			return v
		default:
			return fmt.Sprint(v)
		}
	case "unsigned8", "unsigned16", "unsigned32":
		return decodeUint32(val)
	case "unsigned64":
		return decodeUint64(val)
	case "signed8", "signed16", "signed32":
		return int32(decodeSigned64(val, def.Length))
	case "signed64":
		return decodeSigned64(val, def.Length)
	case "bytes", "macAddress":
		return cloneRawBytes(val)
	default:
		return cloneRawBytes(val)
	}
}

func decodeSigned64(val any, length uint16) int64 {
	switch v := val.(type) {
	case []byte:
		u := decodeUint64(v)
		bits := uint(length) * 8
		if bits == 0 {
			bits = uint(len(v)) * 8
		}
		if bits == 0 || bits >= 64 {
			return int64(u)
		}
		sign := uint64(1) << (bits - 1)
		if u&sign == 0 {
			return int64(u)
		}
		mask := uint64(math.MaxUint64) << bits
		return int64(u | mask)
	case int64:
		return v
	case int32:
		return int64(v)
	case uint64:
		return int64(v)
	case uint32:
		return int64(v)
	default:
		return 0
	}
}

func cloneRawBytes(val any) any {
	raw, ok := val.([]byte)
	if !ok {
		return val
	}
	return append([]byte(nil), raw...)
}
