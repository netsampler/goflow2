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
	Payload    any             `json:"-"`
}

type SourceMetadata struct {
	Network string       `json:"network"`
	Address string       `json:"address"`
	Remote  string       `json:"remote,omitempty"`
	Type    string       `json:"type,omitempty"`
	JSON    JSONMetadata `json:"json,omitempty"`
}

type JSONMetadata struct {
	Flavor string `json:"flavor,omitempty"`
}
