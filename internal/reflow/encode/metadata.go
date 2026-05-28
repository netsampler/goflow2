package encode

import (
	"net/netip"

	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

func eventAgentIP(evt *event.Event) string {
	if evt == nil {
		return ""
	}
	if evt.SFlow != nil && evt.SFlow.AgentIP != "" {
		return evt.SFlow.AgentIP
	}
	if evt.Source.AgentIP != "" {
		return evt.Source.AgentIP
	}
	return ""
}

func eventSamplingRate(evt *event.Event) uint32 {
	if evt == nil {
		return 0
	}
	if evt.SFlow != nil && evt.SFlow.SamplingRate != 0 {
		return evt.SFlow.SamplingRate
	}
	if evt.Source.Sampling != nil && evt.Source.Sampling.Rate != 0 {
		return evt.Source.Sampling.Rate
	}
	return 0
}

func eventSourceID(evt *event.Event) uint32 {
	if evt == nil {
		return 0
	}
	if evt.SFlow != nil && evt.SFlow.SourceID != 0 {
		return evt.SFlow.SourceID
	}
	if evt.Source.SourceID != 0 {
		return evt.Source.SourceID
	}
	return 0
}

func eventSamplePool(evt *event.Event) uint32 {
	if evt == nil {
		return 0
	}
	if evt.SFlow != nil && evt.SFlow.SamplePool != 0 {
		return evt.SFlow.SamplePool
	}
	if evt.Source.Sampling != nil && evt.Source.Sampling.SamplePool != 0 {
		return evt.Source.Sampling.SamplePool
	}
	return 0
}

func eventDrops(evt *event.Event) uint32 {
	if evt == nil {
		return 0
	}
	if evt.SFlow != nil && evt.SFlow.Drops != 0 {
		return evt.SFlow.Drops
	}
	if evt.Source.Sampling != nil && evt.Source.Sampling.Drops != 0 {
		return evt.Source.Sampling.Drops
	}
	return 0
}

func eventFieldsWithMetadata(evt *event.Event) map[string]any {
	if evt == nil {
		return nil
	}
	fields := cloneFieldMap(evt.Fields)
	addEventMetadataFields(fields, evt, nil)
	return fields
}

func eventFieldsWithMetadataForSchema(evt *event.Event, schemaFields []event.SchemaField) map[string]any {
	if evt == nil {
		return nil
	}
	fields := cloneFieldMap(evt.Fields)
	names := make(map[string]struct{}, len(schemaFields))
	for _, field := range schemaFields {
		names[field.Name] = struct{}{}
	}
	addEventMetadataFields(fields, evt, names)
	return fields
}

func cloneFieldMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return make(map[string]any)
	}
	out := make(map[string]any, len(in)+6)
	for key, val := range in {
		out[key] = val
	}
	return out
}

func addEventMetadataFields(fields map[string]any, evt *event.Event, names map[string]struct{}) {
	add := func(name string, val any, present bool) {
		if !present {
			return
		}
		if names != nil {
			if _, ok := names[name]; !ok {
				return
			}
		}
		fields[name] = val
	}
	agentIP := eventAgentIP(evt)
	agentIPv4, agentIPv6 := agentIPByFamily(agentIP)
	sourceID := eventSourceID(evt)
	samplingRate := eventSamplingRate(evt)
	samplePool := eventSamplePool(evt)
	drops := eventDrops(evt)
	add("agent_ip", agentIPv4, agentIPv4 != "")
	add("agent_ipv6", agentIPv6, agentIPv6 != "")
	add("source_id", sourceID, sourceID != 0)
	add("sampling_rate", samplingRate, hasSamplingMetadata(evt))
	add("sample_pool", samplePool, hasSamplePoolMetadata(evt))
	add("drops", drops, hasDropsMetadata(evt))
}

func agentIPByFamily(raw string) (string, string) {
	if raw == "" {
		return "", ""
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return raw, ""
	}
	if addr.Is4() {
		return raw, ""
	}
	return "", raw
}

func hasSamplingMetadata(evt *event.Event) bool {
	return evt != nil && ((evt.SFlow != nil && evt.SFlow.SamplingRate != 0) || evt.Source.Sampling != nil)
}

func hasSamplePoolMetadata(evt *event.Event) bool {
	return evt != nil && ((evt.SFlow != nil && evt.SFlow.SamplePool != 0) || evt.Source.Sampling != nil)
}

func hasDropsMetadata(evt *event.Event) bool {
	return evt != nil && ((evt.SFlow != nil && evt.SFlow.Drops != 0) || evt.Source.Sampling != nil)
}
