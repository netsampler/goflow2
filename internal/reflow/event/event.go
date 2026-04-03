package event

import (
	"encoding/json"
	"time"
)

type Event struct {
	ReceivedAt time.Time       `json:"received_at"`
	Source     SourceMetadata  `json:"source"`
	Message    json.RawMessage `json:"message"`
	Fields     map[string]any  `json:"fields,omitempty"`
}

type SourceMetadata struct {
	Network string `json:"network"`
	Address string `json:"address"`
	Remote  string `json:"remote,omitempty"`
	Frame   string `json:"frame"`
}
