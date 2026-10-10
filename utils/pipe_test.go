package utils

import (
	"encoding/json"
	"testing"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
	"github.com/stretchr/testify/require"
)

func TestTemplateSnapshotFormatting(t *testing.T) {
	store := templates.NewTemplateFlowStore()
	t.Cleanup(store.Close)
	for _, entry := range []struct {
		router   string
		version  uint16
		domain   uint32
		id       uint16
		template interface{}
	}{
		{"router1", 9, 0, 256, netflow.TemplateRecord{TemplateId: 256}},
		{"router1", 9, 1, 257, netflow.NFv9OptionsTemplateRecord{TemplateId: 257}},
		{"router1", 10, 1, 257, netflow.IPFIXOptionsTemplateRecord{TemplateId: 257}},
		{"router2", 9, 0, 256, netflow.TemplateRecord{TemplateId: 256, FieldCount: 1}},
		{"router2", 65535, 4294967295, 65535, netflow.TemplateRecord{TemplateId: 65535}},
	} {
		_, err := store.AddTemplate(netflow.FlowContext{RouterKey: entry.router}, entry.version, entry.domain, entry.id, entry.template)
		require.NoError(t, err)
	}
	pipe := NewNetFlowPipe(&PipeConfig{TemplateStore: store})
	snapshot := pipe.GetTemplatesForAllSources()
	require.Equal(t, map[string]map[string]interface{}{
		"router1": {
			"9/0/256":  netflow.TemplateRecord{TemplateId: 256},
			"9/1/257":  netflow.NFv9OptionsTemplateRecord{TemplateId: 257},
			"10/1/257": netflow.IPFIXOptionsTemplateRecord{TemplateId: 257},
		},
		"router2": {
			"9/0/256":                netflow.TemplateRecord{TemplateId: 256, FieldCount: 1},
			"65535/4294967295/65535": netflow.TemplateRecord{TemplateId: 65535},
		},
	}, snapshot)
	pipeJSON, err := json.Marshal(snapshot)
	require.NoError(t, err)
	persistedJSON, err := templates.MarshalJSONSnapshot(store)
	require.NoError(t, err)
	require.JSONEq(t, string(pipeJSON), string(persistedJSON))

	reloaded := templates.NewTemplateFlowStore()
	t.Cleanup(reloaded.Close)
	require.NoError(t, templates.LoadJSON(reloaded, persistedJSON))
	require.Equal(t, store.GetAll(), reloaded.GetAll())
}
