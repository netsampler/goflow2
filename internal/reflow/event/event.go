package event

import (
	"encoding/json"
	"time"
)

type Event struct {
	ReceivedAt time.Time       `json:"received_at"`
	Source     SourceMetadata  `json:"source"`
	Message    json.RawMessage `json:"message,omitempty"`
	Fields     map[string]any  `json:"fields,omitempty"`
	SFlow      *SFlowMetadata  `json:"sflow,omitempty"`
	Payload    any             `json:"-"`
}

type SourceMetadata struct {
	Network               string       `json:"network"`
	Address               string       `json:"address"`
	Remote                string       `json:"remote,omitempty"`
	Type                  string       `json:"type,omitempty"`
	CaptureInterface      string       `json:"capture_interface,omitempty"`
	CaptureInterfaceIndex int          `json:"capture_interface_index,omitempty"`
	JSON                  JSONMetadata `json:"json,omitempty"`
}

type JSONMetadata struct {
	Flavor string `json:"flavor,omitempty"`
}

type SFlowMetadata struct {
	AgentIP        string `json:"agent_ip,omitempty"`
	SubAgentID     uint32 `json:"sub_agent_id,omitempty"`
	SequenceNumber uint32 `json:"sequence_number,omitempty"`
	Uptime         uint32 `json:"uptime,omitempty"`
	SourceID       uint32 `json:"source_id,omitempty"`
	SamplingRate   uint32 `json:"sampling_rate,omitempty"`
	SamplePool     uint32 `json:"sample_pool,omitempty"`
	Drops          uint32 `json:"drops,omitempty"`
}
