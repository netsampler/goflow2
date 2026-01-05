package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
)

func TestInMemoryRegistryKeepsEmptySystem(t *testing.T) {
	registry := NewInMemoryRegistry(nil)
	system := registry.GetSystem("router1")

	if _, err := system.AddTemplate(9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}
	if _, _, err := system.RemoveTemplate(9, 1, 256); err != nil {
		t.Fatalf("remove template: %v", err)
	}
	all := registry.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 system after removal, got %d", len(all))
	}
	if len(all["router1"]) != 0 {
		t.Fatalf("expected empty system after removal, got %d", len(all["router1"]))
	}
}

func TestInMemoryRegistryRetainsNonEmptySystem(t *testing.T) {
	registry := NewInMemoryRegistry(nil)
	system := registry.GetSystem("router1")

	if _, err := system.AddTemplate(9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}
	if _, err := system.AddTemplate(9, 1, 257, netflow.TemplateRecord{TemplateId: 257}); err != nil {
		t.Fatalf("add template: %v", err)
	}
	if _, _, err := system.RemoveTemplate(9, 1, 256); err != nil {
		t.Fatalf("remove template: %v", err)
	}

	all := registry.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 system, got %d", len(all))
	}
	if len(all["router1"]) != 1 {
		t.Fatalf("expected 1 template in system, got %d", len(all["router1"]))
	}
}

func TestExpiringRegistryExpiresAndPrunes(t *testing.T) {
	base := NewInMemoryRegistry(nil)
	registry := NewExpiringRegistry(base, time.Minute)

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	registry.now = func() time.Time { return now }

	system := registry.GetSystem("router1")
	if _, err := system.AddTemplate(9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}

	now = start.Add(2 * time.Minute)
	if removed, pruned := registry.ExpireStale(); removed != 1 || pruned != 0 {
		t.Fatalf("expected 1 template expired and 0 systems pruned, got %d/%d", removed, pruned)
	}
	if got := registry.GetAll(); len(got) != 1 {
		t.Fatalf("expected system retained after template expiry, got %d", len(got))
	}

	now = start.Add(3*time.Minute + time.Second)
	if removed, pruned := registry.ExpireStale(); removed != 0 || pruned != 1 {
		t.Fatalf("expected no templates expired and 1 system pruned, got %d/%d", removed, pruned)
	}
	if got := registry.GetAll(); len(got) != 0 {
		t.Fatalf("expected empty system pruned after TTL, got %d", len(got))
	}
}

func TestExpiringRegistryExtendsOnAccess(t *testing.T) {
	base := NewInMemoryRegistry(nil)
	registry := NewExpiringRegistry(base, time.Minute, WithExtendOnAccess(true))

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	registry.now = func() time.Time { return now }

	system := registry.GetSystem("router1")
	if _, err := system.AddTemplate(9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}

	now = start.Add(30 * time.Second)
	if _, err := system.GetTemplate(9, 1, 256); err != nil {
		t.Fatalf("get template: %v", err)
	}

	now = start.Add(90 * time.Second)
	if removed, pruned := registry.ExpireStale(); removed != 0 || pruned != 0 {
		t.Fatalf("expected no templates expired and 0 systems pruned, got %d/%d", removed, pruned)
	}
	if got := registry.GetAll(); len(got) != 1 {
		t.Fatalf("expected templates retained after access, got %d", len(got))
	}
}

func TestJSONRegistryPersistAndPrune(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "templates.json")

	base := NewInMemoryRegistry(nil)
	registry := NewJSONRegistry(path, base, WithJSONFlushInterval(0))
	t.Cleanup(registry.Close)

	system := registry.GetSystem("router1")
	if _, err := system.AddTemplate(9, 1, 256, netflow.TemplateRecord{TemplateId: 256}); err != nil {
		t.Fatalf("add template: %v", err)
	}

	raw := readRegistryFile(t, path)
	if len(raw) != 1 {
		t.Fatalf("expected 1 router persisted, got %d", len(raw))
	}

	if _, _, err := system.RemoveTemplate(9, 1, 256); err != nil {
		t.Fatalf("remove template: %v", err)
	}

	raw = readRegistryFile(t, path)
	if len(raw) != 0 {
		t.Fatalf("expected persisted data to be empty after prune, got %d", len(raw))
	}
}

func TestPreloadJSONTemplates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "templates.json")

	keyString := buildTemplateKeyString(9, 1, 256)
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

	registry := NewInMemoryRegistry(nil)
	if err := PreloadJSONTemplates(path, registry); err != nil {
		t.Fatalf("preload templates: %v", err)
	}

	all := registry.GetAll()
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

func readRegistryFile(t *testing.T, path string) map[string]map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(data) == 0 {
		return map[string]map[string]json.RawMessage{}
	}
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal file: %v", err)
	}
	return raw
}

func buildTemplateKeyString(version uint16, obsDomainId uint32, templateId uint16) string {
	return strconv.FormatUint(uint64(version), 10) + "/" +
		strconv.FormatUint(uint64(obsDomainId), 10) + "/" +
		strconv.FormatUint(uint64(templateId), 10)
}

func buildTemplateKeyUint(version uint16, obsDomainId uint32, templateId uint16) uint64 {
	return (uint64(version) << 48) | (uint64(obsDomainId) << 16) | uint64(templateId)
}
