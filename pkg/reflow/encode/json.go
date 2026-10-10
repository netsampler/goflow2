package encode

import (
	"encoding/json"
	"fmt"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

type JSONEncoder struct {
	flavor     string
	dropFields map[string]struct{}
}

// NewJSONEncoder creates the stateless JSON event encoder.
func NewJSONEncoder(cfg config.EncoderConfig) *JSONEncoder {
	dropFields := make(map[string]struct{}, len(cfg.JSON.DropFields))
	for _, field := range cfg.JSON.DropFields {
		dropFields[field] = struct{}{}
	}
	return &JSONEncoder{
		flavor:     cfg.JSON.Flavor,
		dropFields: dropFields,
	}
}

// Encode serializes one event as a JSON payload in the configured output flavor.
func (e JSONEncoder) Encode(evt *event.Event) ([][]byte, error) {
	payload := e.formatEvent(evt)
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal event: %w", err)
	}
	return [][]byte{data}, nil
}

// Flush is a no-op for JSON because it does not keep internal batching state.
func (JSONEncoder) Flush() ([][]byte, error) {
	return nil, nil
}

// formatEvent selects the output view used for one JSON-encoded event.
func (e JSONEncoder) formatEvent(evt *event.Event) any {
	switch e.flavor {
	case "", "canonical":
		return e.filterEvent(evt)
	case "vendor":
		return e.filterMap(map[string]any{
			"src_addr":         stringFieldOrZero(evt.Fields, "src_addr"),
			"dst_addr":         stringFieldOrZero(evt.Fields, "dst_addr"),
			"src_port":         uint32Field(evt.Fields, "src_port"),
			"dst_port":         uint32Field(evt.Fields, "dst_port"),
			"proto":            uint32Field(evt.Fields, "proto"),
			"packets":          int64Field(evt.Fields, "packets"),
			"bytes":            int64Field(evt.Fields, "bytes"),
			"start_time_unix":  int64Field(evt.Fields, "start_time_unix"),
			"end_time_unix":    int64Field(evt.Fields, "end_time_unix"),
			"flow_direction":   stringFieldOrZero(evt.Fields, "flow_direction"),
			"traffic_decision": stringFieldOrZero(evt.Fields, "traffic_decision"),
			"action":           stringFieldOrZero(evt.Fields, "action"),
			"log_status":       stringFieldOrZero(evt.Fields, "log_status"),
			"reporter":         stringFieldOrZero(evt.Fields, "reporter"),
			"disposition":      stringFieldOrZero(evt.Fields, "disposition"),
		})
	case "goflow2v2":
		out := map[string]any{
			"sampler_address":    encodeIPBytes(eventAgentIP(evt)),
			"src_addr":           encodeIPBytes(stringFieldOrZero(evt.Fields, "src_addr")),
			"dst_addr":           encodeIPBytes(stringFieldOrZero(evt.Fields, "dst_addr")),
			"src_port":           uint32Field(evt.Fields, "src_port"),
			"dst_port":           uint32Field(evt.Fields, "dst_port"),
			"proto":              uint32Field(evt.Fields, "proto"),
			"bytes":              int64Field(evt.Fields, "bytes"),
			"packets":            int64Field(evt.Fields, "packets"),
			"time_flow_start_ns": timeFlowNS(evt.Fields, "time_flow_start_ns", "start_time_unix"),
			"time_flow_end_ns":   timeFlowNS(evt.Fields, "time_flow_end_ns", "end_time_unix"),
			"sampling_rate":      eventSamplingRate(evt),
			"in_if":              uint32Field(evt.Fields, "input_if"),
			"out_if":             uint32Field(evt.Fields, "output_if"),
			"type":               flowTypeField(evt.Fields),
		}
		return e.filterMap(out)
	default:
		return e.filterEvent(evt)
	}
}

// filterEvent preserves the original event when no fields were dropped to avoid
// allocating a shallow copy for the common case.
func (e JSONEncoder) filterEvent(evt *event.Event) any {
	if len(e.dropFields) == 0 || len(evt.Fields) == 0 {
		return evt
	}

	filteredFields := e.filterMap(evt.Fields)
	if len(filteredFields) == len(evt.Fields) {
		return evt
	}

	filtered := *evt
	filtered.Fields = filteredFields
	return &filtered
}

// filterMap applies the configured drop-fields policy to a map payload.
func (e JSONEncoder) filterMap(fields map[string]any) map[string]any {
	if len(fields) == 0 || len(e.dropFields) == 0 {
		return fields
	}

	filtered := make(map[string]any, len(fields))
	for key, value := range fields {
		if _, drop := e.dropFields[key]; drop {
			continue
		}
		filtered[key] = value
	}
	return filtered
}
