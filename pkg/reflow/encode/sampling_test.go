package encode

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
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

func TestIPFIXSelectorOptionsFitBatchDatagrams(t *testing.T) {
	enabled := true
	const limit = 100
	enc := NewIPFIXEncoder(config.EncoderConfig{
		MaxDatagramBytes: limit,
		Batch:            config.BatchConfig{Enabled: &enabled, MaxRecords: 2},
		TemplatedFlow: config.TemplatedFlowConfig{
			SamplingMode: config.IPFIXSamplingSelector,
			Data: config.TemplatedFlowDataConfig{
				Catalog: map[string]config.IPFIXFieldDefinition{
					"bytes": {ID: netflow.IPFIX_FIELD_octetDeltaCount, Length: 8, Type: "unsigned64"},
				},
			},
		},
	})
	rates := []uint32{64, 256}
	var payloads [][]byte
	for _, rate := range rates {
		data, err := enc.Encode(&event.Event{Fields: map[string]any{"bytes": uint64(1000), "sampling_rate": rate}})
		if err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, data...)
	}
	flushed, err := enc.Flush()
	if err != nil {
		t.Fatal(err)
	}
	payloads = append(payloads, flushed...)
	if len(payloads) != len(rates) {
		t.Fatalf("expected one datagram per rate at %d bytes, got %d datagrams", limit, len(payloads))
	}
	store := templates.NewTemplateFlowStore()
	defer store.Close()
	for i, payload := range payloads {
		if len(payload) > limit {
			t.Fatalf("datagram %d is %d bytes, limit %d", i, len(payload), limit)
		}
		var packet netflow.IPFIXPacket
		if err := netflow.DecodeMessageVersion(bytes.NewBuffer(payload), store, netflow.FlowContext{}, nil, &packet); err != nil {
			t.Fatal(err)
		}
		options := packet.FlowSets[1].(netflow.OptionsDataFlowSet)
		data := packet.FlowSets[3].(netflow.DataFlowSet)
		rate := netflow.IPFIXSelectorsFromOptions([]netflow.OptionsDataFlowSet{options}).Rate(data.Records[0].Values)
		if rate != rates[i] || packet.SequenceNumber != uint32(i*2) {
			t.Fatalf("datagram %d has rate %d and sequence %d", i, rate, packet.SequenceNumber)
		}
	}
}
