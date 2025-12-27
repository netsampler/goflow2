package templates

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

func TestTemplateSystemRegistryPreloadSources(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/templates.json"

	file := jsonTemplateFile{
		Routers: map[string]jsonRouterTemplates{
			"router-a": {},
		},
	}
	payload, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write JSON: %v", err)
	}

	writer := NewAtomicFileWriter(path)
	generator := NewJSONFileTemplateSystemGenerator(writer, DefaultTemplateGenerator, 0)
	registry := NewTemplateSystemRegistry(DefaultTemplateGenerator, 0, 0)

	if err := registry.PreloadSources(generator); err != nil {
		t.Fatalf("preload sources: %v", err)
	}

	snapshot := registry.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("expected 1 template system, got %d", len(snapshot))
	}
	if _, ok := snapshot["router-a"]; !ok {
		t.Fatalf("expected router-a to be preloaded")
	}
}

func TestTemplateSystemRegistryEvictStale(t *testing.T) {
	registry := NewTemplateSystemRegistry(DefaultTemplateGenerator, time.Second, time.Minute)

	emptyKey := "empty-router"
	registry.Get(emptyKey)

	staleKey := "stale-router"
	system := registry.Get(staleKey)
	template := netflow.TemplateRecord{
		TemplateId: 256,
		FieldCount: 1,
		Fields: []netflow.Field{
			{Type: 1, Length: 4},
		},
	}
	if err := system.AddTemplate(9, 1, 256, template); err != nil {
		t.Fatalf("add template: %v", err)
	}

	registry.lock.Lock()
	registry.lastSeen[staleKey] = time.Now().Add(-2 * time.Second)
	registry.lock.Unlock()

	registry.evictStale()

	registry.lock.RLock()
	_, emptyExists := registry.templates[emptyKey]
	_, staleExists := registry.templates[staleKey]
	registry.lock.RUnlock()

	if emptyExists {
		t.Fatalf("expected empty template system to be evicted")
	}
	if staleExists {
		t.Fatalf("expected stale template system to be evicted")
	}
}

func TestTemplateSystemRegistryEvictLoop(t *testing.T) {
	registry := NewTemplateSystemRegistry(DefaultTemplateGenerator, 10*time.Millisecond, 5*time.Millisecond)
	t.Cleanup(registry.Close)

	key := "loop-router"
	system := registry.Get(key)
	template := netflow.TemplateRecord{
		TemplateId: 256,
		FieldCount: 1,
		Fields: []netflow.Field{
			{Type: 1, Length: 4},
		},
	}
	if err := system.AddTemplate(9, 1, 256, template); err != nil {
		t.Fatalf("add template: %v", err)
	}

	registry.lock.Lock()
	registry.lastSeen[key] = time.Now().Add(-time.Second)
	registry.lock.Unlock()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		registry.lock.RLock()
		_, ok := registry.templates[key]
		registry.lock.RUnlock()
		if !ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("expected template system to be evicted by evictLoop")
}
