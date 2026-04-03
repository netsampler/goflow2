package persistence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
	"github.com/netsampler/goflow2/v3/utils/store/samplingrate"
	"github.com/netsampler/goflow2/v3/utils/store/templates"
)

func TestManagerPersistsSamplingRatesAndTemplates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stores.json")

	manager := New(Config{
		Path:     path,
		Interval: 0,
	})
	defer manager.Close()

	samplingStore, err := manager.NewSamplingRateStore()
	if err != nil {
		t.Fatalf("new sampling store: %v", err)
	}
	templateStore, err := manager.NewTemplateStore()
	if err != nil {
		t.Fatalf("new template store: %v", err)
	}

	manager.Start()

	ctx := netflow.FlowContext{RouterKey: "router1"}
	if err := samplingStore.Set(ctx, 9, 1, 100); err != nil {
		t.Fatalf("set sampling rate: %v", err)
	}
	if _, err := templateStore.AddTemplate(ctx, 9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}

	waitForDocumentKeys(t, path, 2)
	waitForSectionScopes(t, path, "sampling-rates", 1)
	waitForSectionScopes(t, path, "templates", 1)

	if _, _, err := samplingStore.Remove(ctx, 9, 1); err != nil {
		t.Fatalf("remove sampling rate: %v", err)
	}
	if _, _, err := templateStore.RemoveTemplate(ctx, 9, 1, 256); err != nil {
		t.Fatalf("remove template: %v", err)
	}

	waitForDocumentKeys(t, path, 2)
	if got := waitForSectionScopes(t, path, "sampling-rates", 0); len(got) != 0 {
		t.Fatalf("expected sampling-rates persistence to be empty, got %d scopes", len(got))
	}
	if got := waitForSectionScopes(t, path, "templates", 0); len(got) != 0 {
		t.Fatalf("expected templates persistence to be empty, got %d scopes", len(got))
	}
}

func TestManagerPreloadsSamplingRatesAndTemplates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stores.json")

	payload, err := json.Marshal(map[string]any{
		"sampling-rates": map[string]map[string]uint32{
			"router1": {
				"9/1": 100,
			},
		},
		"templates": map[string]map[string]netflow.TemplateRecord{
			"router1": {
				"9/1/256": {TemplateId: 256},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	manager := New(Config{
		Path: path,
	})
	defer manager.Close()

	samplingStore, err := manager.NewSamplingRateStore()
	if err != nil {
		t.Fatalf("new sampling store: %v", err)
	}
	templateStore, err := manager.NewTemplateStore()
	if err != nil {
		t.Fatalf("new template store: %v", err)
	}

	rateStore, ok := samplingStore.(*samplingrate.SamplingRateFlowStore)
	if !ok {
		t.Fatalf("unexpected sampling store type %T", samplingStore)
	}
	allRates := rateStore.GetAll()
	if allRates["router1"][uint64(9)<<32|1] != 100 {
		t.Fatalf("expected preloaded sampling rate")
	}

	tplStore, ok := templateStore.(*templates.TemplateFlowStore)
	if !ok {
		t.Fatalf("unexpected template store type %T", templateStore)
	}
	allTemplates := tplStore.GetAll()
	template, ok := allTemplates["router1"][(uint64(9)<<48)|(uint64(1)<<16)|256]
	if !ok {
		t.Fatalf("expected preloaded template")
	}
	record, ok := template.(netflow.TemplateRecord)
	if !ok || record.TemplateId != 256 {
		t.Fatalf("unexpected preloaded template %#v", template)
	}
}

func TestStoreClosePersistsFinalSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stores.json")

	manager := New(Config{
		Path:     path,
		Interval: time.Hour,
	})
	defer manager.Close()

	samplingStore, err := manager.NewSamplingRateStore()
	if err != nil {
		t.Fatalf("new sampling store: %v", err)
	}
	templateStore, err := manager.NewTemplateStore()
	if err != nil {
		t.Fatalf("new template store: %v", err)
	}

	ctx := netflow.FlowContext{RouterKey: "router1"}
	if err := samplingStore.Set(ctx, 9, 1, 100); err != nil {
		t.Fatalf("set sampling rate: %v", err)
	}
	if _, err := templateStore.AddTemplate(ctx, 9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}

	samplingStore.Close()
	waitForDocumentKeys(t, path, 2)
	if got := waitForSectionScopes(t, path, "sampling-rates", 1); len(got) != 1 {
		t.Fatalf("expected sampling-rates snapshot after sampling close, got %d scopes", len(got))
	}
	if got := waitForSectionScopes(t, path, "templates", 1); len(got) != 1 {
		t.Fatalf("expected templates snapshot after sampling close, got %d scopes", len(got))
	}

	templateStore.Close()
	waitForDocumentKeys(t, path, 2)
	if got := waitForSectionScopes(t, path, "sampling-rates", 1); len(got) != 1 {
		t.Fatalf("expected sampling-rates snapshot after template close, got %d scopes", len(got))
	}
	if got := waitForSectionScopes(t, path, "templates", 1); len(got) != 1 {
		t.Fatalf("expected templates snapshot after template close, got %d scopes", len(got))
	}
}

func readJSONFile(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) == 0 {
		return map[string]json.RawMessage{}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal file: %v", err)
	}
	return raw
}

func waitForDocumentKeys(t *testing.T, path string, want int) map[string]json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			raw := readJSONFile(t, path)
			if len(raw) == want {
				return raw
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return readJSONFile(t, path)
}

func waitForSectionScopes(t *testing.T, path string, key string, want int) map[string]map[string]json.RawMessage {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		document := readJSONFile(t, path)
		section := document[key]
		if len(section) == 0 {
			if want == 0 {
				return map[string]map[string]json.RawMessage{}
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		var raw map[string]map[string]json.RawMessage
		if err := json.Unmarshal(section, &raw); err != nil {
			t.Fatalf("unmarshal section %q: %v", key, err)
		}
		if len(raw) == want {
			return raw
		}
		time.Sleep(10 * time.Millisecond)
	}
	document := readJSONFile(t, path)
	section := document[key]
	if len(section) == 0 && want == 0 {
		return map[string]map[string]json.RawMessage{}
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(section, &raw); err != nil {
		t.Fatalf("unmarshal section %q: %v", key, err)
	}
	t.Fatalf("expected %d scopes in section %q, got %d", want, key, len(raw))
	return nil
}
