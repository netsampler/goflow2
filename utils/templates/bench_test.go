package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v2/decoders/netflow"
)

func newChainedRegistry(b *testing.B) *ExpiringRegistry {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "templates.json")

	base := NewInMemoryRegistry(nil)
	jsonRegistry := NewJSONRegistry(path, time.Hour, base)
	b.Cleanup(jsonRegistry.Close)

	expiring := NewExpiringRegistry(jsonRegistry, time.Minute)
	b.Cleanup(expiring.Close)
	return expiring
}

func newChainedRegistryWithJSONFlush(b *testing.B) *ExpiringRegistry {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "templates.json")

	base := NewInMemoryRegistry(nil)
	jsonRegistry := NewJSONRegistry(path, 0, base)
	b.Cleanup(jsonRegistry.Close)

	expiring := NewExpiringRegistry(jsonRegistry, time.Minute)
	b.Cleanup(expiring.Close)
	return expiring
}

func BenchmarkChainedRegistryAdd(b *testing.B) {
	registry := newChainedRegistry(b)
	system := registry.GetSystem("router1")

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := system.AddTemplate(9, 1, uint16(n), netflow.TemplateRecord{TemplateId: uint16(n)}); err != nil {
			b.Fatalf("add template: %v", err)
		}
	}
}

func BenchmarkChainedRegistryAddGet(b *testing.B) {
	registry := newChainedRegistry(b)
	system := registry.GetSystem("router1")

	const templates = 1000
	for n := 0; n < templates; n++ {
		if _, err := system.AddTemplate(9, 1, uint16(n), netflow.TemplateRecord{TemplateId: uint16(n)}); err != nil {
			b.Fatalf("add template: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := system.GetTemplate(9, 1, uint16(n%templates)); err != nil {
			b.Fatalf("get template: %v", err)
		}
	}
}

func BenchmarkChainedRegistryAddWithJSONFlush(b *testing.B) {
	registry := newChainedRegistryWithJSONFlush(b)
	system := registry.GetSystem("router1")

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := system.AddTemplate(9, 1, uint16(n), netflow.TemplateRecord{TemplateId: uint16(n)}); err != nil {
			b.Fatalf("add template: %v", err)
		}
	}
}

func BenchmarkChainedRegistryPreloadJSON(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "templates.json")

	const templates = 1000
	payload := map[string]map[string]netflow.TemplateRecord{
		"router1": {},
	}
	for n := 0; n < templates; n++ {
		key := buildTemplateKey(9, 1, uint16(n))
		payload["router1"][strconv.FormatUint(key, 10)] = netflow.TemplateRecord{TemplateId: uint16(n)}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		b.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatalf("write payload: %v", err)
	}

	base := NewInMemoryRegistry(nil)
	jsonRegistry := NewJSONRegistry(path, time.Hour, base)
	expiring := NewExpiringRegistry(jsonRegistry, time.Minute)
	if err := PreloadJSONTemplates(path, expiring); err != nil {
		b.Fatalf("preload templates: %v", err)
	}
	b.Cleanup(expiring.Close)

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		templateID := uint16(n % templates)
		if _, err := expiring.GetSystem("router1").GetTemplate(9, 1, templateID); err != nil {
			b.Fatalf("get template: %v", err)
		}
		if _, err := expiring.GetSystem("router1").AddTemplate(9, 1, templateID, netflow.TemplateRecord{TemplateId: templateID}); err != nil {
			b.Fatalf("add template: %v", err)
		}
	}
}
