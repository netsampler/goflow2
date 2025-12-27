package templates

import (
	"encoding/json"
	"os"
	"testing"
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
