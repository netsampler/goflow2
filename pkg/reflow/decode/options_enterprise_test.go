package decode

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/encode"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/stretchr/testify/require"
)

func enterpriseOptionsPayloads(t *testing.T, id uint16, pen uint32, length uint16, kind string, value any) ([][]byte, map[string]config.IPFIXFieldDefinition) {
	t.Helper()
	catalog := map[string]config.IPFIXFieldDefinition{
		"observation_domain_id": {ID: 149, Length: 4, Type: "unsigned32"},
		"custom":                {ID: id, PEN: pen, EnterpriseScoped: true, Length: length, Type: kind},
	}
	enc := encode.NewIPFIXEncoder(config.EncoderConfig{Type: "ipfix", TemplatedFlow: config.TemplatedFlowConfig{OptionsTemplateBaseID: 1300, Data: config.TemplatedFlowDataConfig{Catalog: catalog}}})
	templates, err := enc.Encode(&event.Event{Kind: "control", Control: &event.ControlMetadata{Type: "schema", Stream: "enterprise_options"}, Payload: event.AggregationSchema{
		Stream: "enterprise_options", BaseTemplateID: 1300, Fields: []event.SchemaField{{Role: "static", Name: "tflow_record_type", Value: "options"}, {Role: "key", Name: "observation_domain_id"}, {Role: "current", Name: "custom"}},
	}})
	require.NoError(t, err)
	require.Len(t, templates, 1)
	data, err := enc.Encode(&event.Event{ReceivedAt: time.Unix(1, 0), Stream: "enterprise_options", Fields: map[string]any{"observation_domain_id": uint32(777), "custom": value}})
	require.NoError(t, err)
	require.Len(t, data, 1)
	return append(templates, data...), catalog
}

func TestIPFIXEnterpriseOptionsWireEncoding(t *testing.T) {
	value := bytes.Repeat([]byte{'A'}, 32)
	payloads, _ := enterpriseOptionsPayloads(t, 12232, 9, 32, "string", string(value))
	// Inspect wire bytes independently of the decoder to verify the E bit and PEN.
	require.Equal(t, uint16(3), binary.BigEndian.Uint16(payloads[0][16:18]))
	require.Contains(t, string(payloads[0][20:]), string([]byte{0xaf, 0xc8, 0, 32, 0, 0, 0, 9}))
	require.Equal(t, append([]byte{0, 0, 3, 9}, value...), payloads[1][20:])
}

func TestIPFIXEnterpriseOptionsCatalogRoundTrip(t *testing.T) {
	for _, pen := range []uint32{0, 9} {
		t.Run(fmt.Sprintf("pen_%d", pen), func(t *testing.T) {
			want := string(bytes.Repeat([]byte{'A'}, 32))
			payloads, catalog := enterpriseOptionsPayloads(t, 12232, pen, 32, "string", want)
			dec := NewWithCatalog(catalog)
			defer dec.Close()
			var options *event.Event
			for _, payload := range payloads {
				items, err := dec.Decode(&event.Event{Source: event.SourceMetadata{Type: "flow"}, Payload: payload})
				require.NoError(t, err)
				for _, item := range items {
					if item.Fields["flow_type"] == "ipfix_options_data" {
						options = item
					}
				}
			}
			require.NotNil(t, options)
			require.Equal(t, want, options.Fields["custom"])
		})
	}
}

func TestIPFIXEnterpriseOptionsDoNotSetSampling(t *testing.T) {
	for _, id := range []uint16{34, 50, 305} {
		for _, pen := range []uint32{0, 9} {
			t.Run(fmt.Sprintf("id_%d/pen_%d", id, pen), func(t *testing.T) {
				payloads, _ := enterpriseOptionsPayloads(t, id, pen, 4, "unsigned32", uint32(1000))
				// A standard catalog definition must not match an explicit enterprise PEN zero.
				dec := NewWithCatalog(map[string]config.IPFIXFieldDefinition{"sampling_rate": {ID: id, Length: 4, Type: "unsigned32"}})
				defer dec.Close()
				base := &event.Event{Source: event.SourceMetadata{Type: "flow"}}
				var options *event.Event
				for _, payload := range payloads {
					base.Payload = payload
					items, err := dec.Decode(base)
					require.NoError(t, err)
					for _, item := range items {
						if item.Fields["flow_type"] == "ipfix_options_data" {
							options = item
						}
					}
				}
				require.NotNil(t, options)
				require.NotContains(t, options.Fields, "sampling_rate")
				require.Equal(t, []byte{0, 0, 3, 232}, options.Fields[fmt.Sprintf("option_%d", id)])
				_, found, err := dec.(*builtIn).sampling.Get(netflow.FlowContext{RouterKey: routerKey(base)}, 10, 0)
				require.NoError(t, err)
				require.False(t, found)
			})
		}
	}
}

func TestSamplingOptionsPreferStandardNamespace(t *testing.T) {
	for _, id := range []uint16{34, 50, 305} {
		fields := []netflow.DataField{{PenProvided: true, Pen: 9, Type: id, Value: []byte{0, 0, 3, 232}}, {Type: id, Value: []byte{0, 0, 7, 208}}}
		rate, found, err := searchNetFlowOptionDataSets([]netflow.OptionsDataFlowSet{{Records: []netflow.OptionsDataRecord{{OptionsValues: fields}}}})
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, uint32(2000), rate)
	}
}
