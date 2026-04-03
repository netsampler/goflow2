package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
)

func TestTemplateFlowStoreAddGetRemove(t *testing.T) {
	store := NewTemplateFlowStore()
	ctx := netflow.FlowContext{RouterKey: "router1"}

	if _, err := store.AddTemplate(ctx, 9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}
	if _, err := store.AddTemplate(ctx, 9, 1, 257, netflow.TemplateRecord{TemplateId: 257}); err != nil {
		t.Fatalf("add template: %v", err)
	}
	if _, _, err := store.RemoveTemplate(ctx, 9, 1, 256); err != nil {
		t.Fatalf("remove template: %v", err)
	}

	all := store.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 router, got %d", len(all))
	}
	if len(all["router1"]) != 1 {
		t.Fatalf("expected 1 template, got %d", len(all["router1"]))
	}
}

func TestTemplateFlowStoreExpiresAndPrunes(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	store := NewTemplateFlowStore(
		WithTTL(time.Minute),
		WithNow(func() time.Time { return now }),
	)
	ctx := netflow.FlowContext{RouterKey: "router1"}

	if _, err := store.AddTemplate(ctx, 9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}

	now = start.Add(2 * time.Minute)
	if removed := store.store.ExpireStale(); removed != 1 {
		t.Fatalf("expected 1 template expired, got %d", removed)
	}
	if got := store.GetAll(); len(got) != 0 {
		t.Fatalf("expected templates removed after expiry, got %d", len(got))
	}
}

func TestTemplateFlowStoreExtendsOnAccess(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	store := NewTemplateFlowStore(
		WithTTL(time.Minute),
		WithExtendOnAccess(true),
		WithNow(func() time.Time { return now }),
	)
	ctx := netflow.FlowContext{RouterKey: "router1"}

	if _, err := store.AddTemplate(ctx, 9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}

	now = start.Add(30 * time.Second)
	if _, err := store.GetTemplate(ctx, 9, 1, 256); err != nil {
		t.Fatalf("get template: %v", err)
	}

	now = start.Add(80 * time.Second)
	if removed := store.store.ExpireStale(); removed != 0 {
		t.Fatalf("expected no templates expired, got %d", removed)
	}
	if got := store.GetAll(); len(got) != 1 {
		t.Fatalf("expected templates retained after access, got %d", len(got))
	}
}

func TestPreloadJSONTemplates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "templates.json")

	keyString := formatTemplateKey(9, 1, 256)
	keyUint := buildTemplateKeyUint(9, 1, 256)
	payload := map[string]map[string]netflow.TemplateRecord{
		"router1": {
			keyString: {TemplateId: 256},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	store := NewTemplateFlowStore()
	if err := PreloadJSONTemplates(path, store); err != nil {
		t.Fatalf("preload templates: %v", err)
	}

	all := store.GetAll()
	templates, ok := all["router1"]
	if !ok {
		t.Fatal("expected router1 templates to be loaded")
	}
	entry, ok := templates[keyUint]
	if !ok {
		t.Fatalf("expected template %d to be loaded", keyUint)
	}
	record, ok := entry.(netflow.TemplateRecord)
	if !ok {
		t.Fatalf("expected TemplateRecord, got %T", entry)
	}
	if record.TemplateId != 256 {
		t.Fatalf("expected template id 256, got %d", record.TemplateId)
	}
}

func buildTemplateKeyUint(version uint16, obsDomainId uint32, templateId uint16) uint64 {
	return (uint64(version) << 48) | (uint64(obsDomainId) << 16) | uint64(templateId)
}
