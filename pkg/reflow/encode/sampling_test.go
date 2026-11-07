package encode

import (
	"encoding/json"
	"testing"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func TestJSONGoFlow2V2PreservesAggregatedSamplingRate(t *testing.T) {
	enc := NewJSONEncoder(config.EncoderConfig{JSON: config.JSONConfig{Flavor: "goflow2v2"}})
	payloads, err := enc.Encode(&event.Event{Kind: "data", Source: event.SourceMetadata{Type: "aggregator"}, Fields: map[string]any{"sampling_rate": uint32(16), "bytes": int64(1000), "packets": int64(1)}})
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		SamplingRate uint32 `json:"sampling_rate"`
	}
	if err := json.Unmarshal(payloads[0], &output); err != nil {
		t.Fatal(err)
	}
	if output.SamplingRate != 16 {
		t.Fatalf("aggregated rate = %d, want 16", output.SamplingRate)
	}
}
