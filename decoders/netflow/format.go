package netflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MarshalJSON encodes the packet without triggering MarshalText.
func (p *IPFIXPacket) MarshalJSON() ([]byte, error) {
	return json.Marshal(*p) // this is a trick to avoid having the JSON marshaller defaults to MarshalText
}

// MarshalJSON encodes the packet without triggering MarshalText.
func (p *NFv9Packet) MarshalJSON() ([]byte, error) {
	return json.Marshal(*p) // this is a trick to avoid having the JSON marshaller defaults to MarshalText
}

// MarshalText formats a concise summary of the packet.
func (p *IPFIXPacket) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("IPFIX count:%d seq:%d", len(p.FlowSets), p.SequenceNumber)), nil
}

// MarshalText formats a concise summary of the packet.
func (p *NFv9Packet) MarshalText() ([]byte, error) {
	return []byte(fmt.Sprintf("NetFlowV%d count:%d seq:%d", p.Version, p.Count, p.SequenceNumber)), nil
}

// formatFlowSets shares packet rendering while keeping each protocol's field and scope names.
func formatFlowSets(flowSets []interface{}, version uint16) string {
	typeToString, scopeToString := NFv9TypeToString, NFv9ScopeToString
	if version == 10 {
		typeToString, scopeToString = IPFIXTypeToString, IPFIXTypeToString
	}
	var str strings.Builder
	fmt.Fprintf(&str, "  FlowSets (%v):\n", len(flowSets))
	for i, flowSet := range flowSets {
		var name, body string
		switch flowSet := flowSet.(type) {
		case TemplateFlowSet:
			name, body = "TemplateFlowSet", flowSet.String(typeToString)
		case NFv9OptionsTemplateFlowSet:
			if version == 9 {
				name, body = "OptionsTemplateFlowSet", flowSet.String(typeToString)
			}
		case IPFIXOptionsTemplateFlowSet:
			if version == 10 {
				name, body = "OptionsTemplateFlowSet", flowSet.String(typeToString)
			}
		case DataFlowSet:
			name, body = "DataFlowSet", flowSet.String(typeToString)
		case RawFlowSet:
			name, body = "RawFlowSet", flowSet.String()
		case OptionsDataFlowSet:
			name, body = "OptionsDataFlowSet", flowSet.String(typeToString, scopeToString)
		}
		if name == "" {
			fmt.Fprintf(&str, "    - (unknown type) %v: %v\n", i, flowSet)
			continue
		}
		fmt.Fprintf(&str, "    - %s %v:\n", name, i)
		str.WriteString(body)
	}
	return str.String()
}

func formatTemplateFields(name string, fields []Field, typeToString func(uint16) string) string {
	var str strings.Builder
	fmt.Fprintf(&str, "            %s (%v):\n", name, len(fields))
	for i, field := range fields {
		fmt.Fprintf(&str, "            - %v. %v (%v): %v\n", i, typeToString(field.Type), field.Type, field.Length)
	}
	return str.String()
}
