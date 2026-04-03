package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
)

func newTemplateFlowStore(b *testing.B) *TemplateFlowStore {
	b.Helper()
	registry := NewTemplateFlowStore(
		WithTTL(time.Minute),
	)
	registry.Start()
	b.Cleanup(registry.Close)
	return registry
}

func newTemplateFlowStoreWithPersistence(b *testing.B, interval time.Duration) *TemplateFlowStore {
	b.Helper()
	_ = interval
	registry := NewTemplateFlowStore(
		WithTTL(time.Minute),
	)
	registry.Start()
	b.Cleanup(registry.Close)
	return registry
}

func BenchmarkTemplateFlowStoreAdd(b *testing.B) {
	registry := newTemplateFlowStore(b)
	ctx := netflow.FlowContext{RouterKey: "router1"}

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := registry.AddTemplate(ctx, 9, 1, uint16(n), netflow.TemplateRecord{TemplateId: uint16(n)}); err != nil {
			b.Fatalf("add template: %v", err)
		}
	}
}

func BenchmarkTemplateFlowStoreAddGet(b *testing.B) {
	registry := newTemplateFlowStore(b)
	ctx := netflow.FlowContext{RouterKey: "router1"}

	const templates = 1000
	for n := 0; n < templates; n++ {
		if _, err := registry.AddTemplate(ctx, 9, 1, uint16(n), netflow.TemplateRecord{TemplateId: uint16(n)}); err != nil {
			b.Fatalf("add template: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := registry.GetTemplate(ctx, 9, 1, uint16(n%templates)); err != nil {
			b.Fatalf("get template: %v", err)
		}
	}
}

func BenchmarkTemplateFlowStoreAddWithJSONFlush(b *testing.B) {
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
			registry := newTemplateFlowStoreWithPersistence(b, entry.interval)
			ctx := netflow.FlowContext{RouterKey: "router1"}

			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				if _, err := registry.AddTemplate(ctx, 9, 1, uint16(n), netflow.TemplateRecord{TemplateId: uint16(n)}); err != nil {
					b.Fatalf("add template: %v", err)
				}
			}
		})
	}
}

func BenchmarkTemplateFlowStorePreloadJSON(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "templates.json")

	const templates = 1000
	payload := map[string]map[string]netflow.TemplateRecord{
		"router1": {},
	}
	for n := 0; n < templates; n++ {
		key := formatTemplateKey(9, 1, uint16(n))
		payload["router1"][key] = netflow.TemplateRecord{TemplateId: uint16(n)}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		b.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		b.Fatalf("write payload: %v", err)
	}

	registry := NewTemplateFlowStore(
		WithTTL(time.Minute),
	)
	registry.Start()
	if err := PreloadJSONTemplates(path, registry); err != nil {
		b.Fatalf("preload templates: %v", err)
	}
	b.Cleanup(registry.Close)

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		templateID := uint16(n % templates)
		ctx := netflow.FlowContext{RouterKey: "router1"}
		if _, err := registry.GetTemplate(ctx, 9, 1, templateID); err != nil {
			b.Fatalf("get template: %v", err)
		}
		if _, err := registry.AddTemplate(ctx, 9, 1, templateID, netflow.TemplateRecord{TemplateId: templateID}); err != nil {
			b.Fatalf("add template: %v", err)
		}
	}
}
