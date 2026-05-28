package event

import (
	"encoding/json"
	"time"
)

type Event struct {
	ReceivedAt time.Time        `json:"received_at"`
	Kind       string           `json:"kind,omitempty"`
	Stream     string           `json:"stream,omitempty"`
	Source     SourceMetadata   `json:"source"`
	Control    *ControlMetadata `json:"control,omitempty"`
	Message    json.RawMessage  `json:"message,omitempty"`
	Fields     map[string]any   `json:"fields,omitempty"`
	Packet     *PacketModel     `json:"packet,omitempty"`
	SFlow      *SFlowMetadata   `json:"sflow,omitempty"`
	Payload    any              `json:"-"`
	Internal   map[string]any   `json:"-"`
}

type ControlMetadata struct {
	Type   string `json:"type,omitempty"`
	Stream string `json:"stream,omitempty"`
}

type AggregationSchema struct {
	Stream         string            `json:"stream,omitempty"`
	FieldNames     []string          `json:"field_names,omitempty"`
	Fields         []SchemaField     `json:"fields,omitempty"`
	KeyFields      []string          `json:"key_fields,omitempty"`
	SumFields      []string          `json:"sum_fields,omitempty"`
	FirstFields    []string          `json:"first_fields,omitempty"`
	CurrentFields  []string          `json:"current_fields,omitempty"`
	MinFields      []string          `json:"min_fields,omitempty"`
	MaxFields      []string          `json:"max_fields,omitempty"`
	AndFields      []string          `json:"and_fields,omitempty"`
	Match          map[string]string `json:"match,omitempty"`
	StaticFields   map[string]any    `json:"static_fields,omitempty"`
	BaseTemplateID uint16            `json:"base_template_id,omitempty"`
}

type SchemaField struct {
	Role  string `json:"role,omitempty"`
	Name  string `json:"name,omitempty"`
	Value any    `json:"value,omitempty"`
}

type SourceInit struct {
	Stream              string `json:"stream,omitempty"`
	AgentIP             string `json:"agent_ip,omitempty"`
	SourceID            uint32 `json:"source_id,omitempty"`
	ObservationDomainID uint32 `json:"observation_domain_id,omitempty"`
	SamplingRate        uint32 `json:"sampling_rate,omitempty"`
	SamplePool          uint32 `json:"sample_pool,omitempty"`
	Drops               uint32 `json:"drops,omitempty"`
	InputIf             uint32 `json:"input_if,omitempty"`
	OutputIf            uint32 `json:"output_if,omitempty"`
}

type SourceMetadata struct {
	Network               string            `json:"network"`
	Address               string            `json:"address"`
	Remote                string            `json:"remote,omitempty"`
	Type                  string            `json:"type,omitempty"`
	CaptureInterface      string            `json:"capture_interface,omitempty"`
	CaptureInterfaceIndex int               `json:"capture_interface_index,omitempty"`
	CaptureDirection      string            `json:"capture_direction,omitempty"`
	CapturePacketType     string            `json:"capture_packet_type,omitempty"`
	AgentIP               string            `json:"agent_ip,omitempty"`
	SourceID              uint32            `json:"source_id,omitempty"`
	SourceIDSet           bool              `json:"-"`
	Sampling              *SamplingMetadata `json:"sampling,omitempty"`
	JSON                  JSONMetadata      `json:"json,omitempty"`
}

type JSONMetadata struct {
	Flavor string `json:"flavor,omitempty"`
}

type SamplingMetadata struct {
	Rate       uint32 `json:"rate,omitempty"`
	SamplePool uint32 `json:"sample_pool,omitempty"`
	Drops      uint32 `json:"drops,omitempty"`
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
