package samplingrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/decoders/netflow"
)

func TestSamplingRateFlowStoreSetGetRemove(t *testing.T) {
	store := NewSamplingRateFlowStore()
	ctx := netflow.FlowContext{RouterKey: "router1"}

	if err := store.Set(ctx, 9, 1, 100); err != nil {
		t.Fatalf("set sampling rate: %v", err)
	}
	if err := store.Set(ctx, 9, 2, 200); err != nil {
		t.Fatalf("set sampling rate: %v", err)
	}
	if _, ok, _ := store.Remove(ctx, 9, 1); !ok {
		t.Fatalf("remove sampling rate: expected true")
	}

	all := store.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 router, got %d", len(all))
	}
	if len(all["router1"]) != 1 {
		t.Fatalf("expected 1 entry after remove, got %d", len(all["router1"]))
	}
}

func TestSamplingRateFlowStoreExpires(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	store := NewSamplingRateFlowStore(
		WithTTL(time.Minute),
		WithNow(func() time.Time { return now }),
	)
	ctx := netflow.FlowContext{RouterKey: "router1"}

	if err := store.Set(ctx, 9, 1, 100); err != nil {
		t.Fatalf("set sampling rate: %v", err)
	}

	now = start.Add(2 * time.Minute)
	if removed := store.store.ExpireStale(); removed != 1 {
		t.Fatalf("expected 1 entry expired, got %d", removed)
	}
	if got := store.GetAll(); len(got) != 0 {
		t.Fatalf("expected entries removed after expiry, got %d", len(got))
	}
}

func TestSamplingRateFlowStoreExtendsOnAccess(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	store := NewSamplingRateFlowStore(
		WithTTL(time.Minute),
		WithExtendOnAccess(true),
		WithNow(func() time.Time { return now }),
	)
	ctx := netflow.FlowContext{RouterKey: "router1"}

	if err := store.Set(ctx, 9, 1, 100); err != nil {
		t.Fatalf("set sampling rate: %v", err)
	}

	now = start.Add(30 * time.Second)
	if _, ok, err := store.Get(ctx, 9, 1); err != nil || !ok {
		t.Fatalf("get sampling rate: %v %v", ok, err)
	}

	now = start.Add(80 * time.Second)
	if removed := store.store.ExpireStale(); removed != 0 {
		t.Fatalf("expected no entries expired, got %d", removed)
	}
	if got := store.GetAll(); len(got) != 1 {
		t.Fatalf("expected entries retained after access, got %d", len(got))
	}
}

func TestPreloadJSONSamplingRates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sampling.json")

	payload := map[string]map[string]uint32{
		"router1": {
			"9/1": 100,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	store := NewSamplingRateFlowStore()
	if err := PreloadJSONSamplingRates(path, store); err != nil {
		t.Fatalf("preload sampling rates: %v", err)
	}

	all := store.GetAll()
	rates, ok := all["router1"]
	if !ok {
		t.Fatal("expected router1 sampling rates to be loaded")
	}
	entry, ok := rates[composeSamplingKey(9, 1)]
	if !ok {
		t.Fatalf("expected sampling key present")
	}
	if entry != 100 {
		t.Fatalf("expected sampling rate 100, got %d", entry)
	}
}
