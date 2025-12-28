package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
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

func newChainedRegistryWithJSONInterval(b *testing.B, interval time.Duration) (*ExpiringRegistry, *JSONRegistry) {
	b.Helper()
	dir := b.TempDir()
	path := filepath.Join(dir, "templates.json")

	base := NewInMemoryRegistry(nil)
	jsonRegistry := NewJSONRegistry(path, interval, base)

	expiring := NewExpiringRegistry(jsonRegistry, time.Minute)
	return expiring, jsonRegistry
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
	intervals := []struct {
		name     string
		interval time.Duration
	}{
		{name: "immediate", interval: 0},
		{name: "5ms", interval: 5 * time.Millisecond},
		{name: "50ms", interval: 50 * time.Millisecond},
	}

	for _, entry := range intervals {
		b.Run(entry.name, func(b *testing.B) {
			registry, jsonRegistry := newChainedRegistryWithJSONInterval(b, entry.interval)
			system := registry.GetSystem("router1")

			var flushes atomic.Int64
			jsonRegistry.flushHook = func() {
				flushes.Add(1)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if _, err := system.AddTemplate(9, 1, uint16(n), netflow.TemplateRecord{TemplateId: uint16(n)}); err != nil {
					b.Fatalf("add template: %v", err)
				}
			}
			b.StopTimer()

			if entry.interval > 0 {
				time.Sleep(entry.interval * 2)
			}

			totalFlushes := flushes.Load()
			b.ReportMetric(float64(totalFlushes), "flushes")
			if b.N > 0 {
				b.ReportMetric(float64(totalFlushes)/float64(b.N), "flush/op")
			}

			jsonRegistry.flushHook = nil
			registry.Close()
		})
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
