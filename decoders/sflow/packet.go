package sflow

import "github.com/netsampler/goflow2/v2/decoders/utils"

type Packet struct {
	Version        uint32          `json:"version"`
	IPVersion      uint32          `json:"ip-version"`
	AgentIP        utils.IPAddress `json:"agent-ip"`
	SubAgentId     uint32          `json:"sub-agent-id"`
	SequenceNumber uint32          `json:"sequence-number"`
	Uptime         uint32          `json:"uptime"`
	SamplesCount   uint32          `json:"samples-count"`
	Samples        []interface{}   `json:"samples"`
}

type SampleHeader struct {
	Format uint32 `json:"format"`
	Length uint32 `json:"length"`

	SampleSequenceNumber uint32 `json:"sample-sequence-number"`
	SourceIdType         uint32 `json:"source-id-type"`
	SourceIdValue        uint32 `json:"source-id-value"`
}

type FlowSample struct {
	Header SampleHeader `json:"header"`

	SamplingRate     uint32       `json:"sampling-rate"`
	SamplePool       uint32       `json:"sample-pool"`
	Drops            uint32       `json:"drops"`
	Input            uint32       `json:"input"`
	Output           uint32       `json:"output"`
	FlowRecordsCount uint32       `json:"flow-records-count"`
	Records          []FlowRecord `json:"records"`
}

type CounterSample struct {
	Header SampleHeader `json:"header"`

	CounterRecordsCount uint32          `json:"counter-records-count"`
	Records             []CounterRecord `json:"records"`
}

type ExpandedFlowSample struct {
	Header SampleHeader `json:"header"`

	SamplingRate     uint32       `json:"sampling-rate"`
	SamplePool       uint32       `json:"sample-pool"`
	Drops            uint32       `json:"drops"`
	InputIfFormat    uint32       `json:"input-if-format"`
	InputIfValue     uint32       `json:"input-if-value"`
	OutputIfFormat   uint32       `json:"output-if-format"`
	OutputIfValue    uint32       `json:"output-if-value"`
	FlowRecordsCount uint32       `json:"flow-records-count"`
	Records          []FlowRecord `json:"records"`
}

// DropSample data structure according to https://sflow.org/sflow_drops.txt
type DropSample struct {
	Header SampleHeader `json:"header"`

	Drops            uint32       `json:"drops"`
	Input            uint32       `json:"input"`
	Output           uint32       `json:"output"`
	Reason           uint32       `json:"reason"`
	FlowRecordsCount uint32       `json:"flow-records-count"`
	Records          []FlowRecord `json:"records"`
}

type RecordHeader struct {
	DataFormat uint32 `json:"data-format"`
	Length     uint32 `json:"length"`
}

type FlowRecord struct {
	Header RecordHeader `json:"header"`
	Data   interface{}  `json:"data"`
}

type CounterRecord struct {
	Header RecordHeader `json:"header"`
	Data   interface{}  `json:"data"`
}
