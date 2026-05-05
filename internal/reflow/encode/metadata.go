package encode

import "github.com/netsampler/goflow2/v3/internal/reflow/event"

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
	return stringFieldOrZero(evt.Fields, "agent_ip")
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
	return uint32Field(evt.Fields, "sampling_rate")
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
	return uint32Field(evt.Fields, "source_id")
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
	return uint32Field(evt.Fields, "sample_pool")
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
	return uint32Field(evt.Fields, "drops")
}
