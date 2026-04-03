package flowstore

import (
	"testing"
	"time"
)

type testValue struct {
	Packets *int64
	Bytes   *int64
	Started *int64
	Ended   *int64
}

func (t *testValue) Add(delta testValue, _ bool) error {
	if delta.Packets != nil {
		if t.Packets == nil {
			v := int64(0)
			t.Packets = &v
		}
		*t.Packets += *delta.Packets
	}
	if delta.Bytes != nil {
		if t.Bytes == nil {
			v := int64(0)
			t.Bytes = &v
		}
		*t.Bytes += *delta.Bytes
	}
	return nil
}

func (t *testValue) Set(val testValue, _ bool) error {
	if val.Packets != nil {
		v := *val.Packets
		t.Packets = &v
	}
	if val.Bytes != nil {
		v := *val.Bytes
		t.Bytes = &v
	}
	if val.Started != nil {
		v := *val.Started
		t.Started = &v
	}
	if val.Ended != nil {
		v := *val.Ended
		t.Ended = &v
	}
	return nil
}

func (t *testValue) CopyFrom(src testValue) {
	_ = t.Set(src, true)
}

// Interface guards are compile-time assertions that testValue satisfies the
// FlowStore interfaces exercised by these tests.
var (
	_ Addable[testValue]  = (*testValue)(nil)
	_ Settable[testValue] = (*testValue)(nil)
	_ Copyable[testValue] = (*testValue)(nil)
)

func ptr(v int64) *int64 {
	return &v
}

func TestStoreAddSetGet(t *testing.T) {
	store := NewStore[string, testValue]()

	if err := store.Add("k1", testValue{Packets: ptr(10), Bytes: ptr(5)}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if existed, err := store.Set("k1", testValue{Started: ptr(100), Ended: ptr(200)}); err != nil {
		t.Fatalf("set: %v", err)
	} else if !existed {
		t.Fatalf("expected Set to update existing key")
	}

	var got testValue
	if !store.Get("k1", &got) {
		t.Fatalf("expected key to exist")
	}
	if got.Packets == nil || *got.Packets != 10 {
		t.Fatalf("expected packets 10, got %#v", got.Packets)
	}
	if got.Bytes == nil || *got.Bytes != 5 {
		t.Fatalf("expected bytes 5, got %#v", got.Bytes)
	}
	if got.Started == nil || *got.Started != 100 {
		t.Fatalf("expected started 100, got %#v", got.Started)
	}
	if got.Ended == nil || *got.Ended != 200 {
		t.Fatalf("expected ended 200, got %#v", got.Ended)
	}
}

func TestStoreFIFOEviction(t *testing.T) {
	store := NewStore[int, testValue](WithMaxSize[int, testValue](2))

	_, _ = store.Set(1, testValue{Packets: ptr(1)})
	_, _ = store.Set(2, testValue{Packets: ptr(2)})
	_, _ = store.Set(3, testValue{Packets: ptr(3)})

	if store.Len() != 2 {
		t.Fatalf("expected len 2, got %d", store.Len())
	}
	var got testValue
	if store.Get(1, &got) {
		t.Fatalf("expected key 1 to be evicted")
	}
	if !store.Get(2, &got) || !store.Get(3, &got) {
		t.Fatalf("expected keys 2 and 3 to exist")
	}
}

func TestStoreTTLExpireHookExtend(t *testing.T) {
	now := time.Unix(0, 0)
	clock := now
	extended := false
	store := NewStore[string, testValue](
		WithDefaultTTL[string, testValue](time.Second),
		WithNow[string, testValue](func() time.Time { return clock }),
		WithExpireHook[string, testValue](func(key string, val testValue) (bool, time.Duration) {
			if key == "k1" && !extended {
				extended = true
				return true, 2 * time.Second
			}
			return false, 0
		}),
	)

	_, _ = store.Set("k1", testValue{Packets: ptr(1)})
	clock = clock.Add(1500 * time.Millisecond)

	var got testValue
	if !store.Get("k1", &got) {
		t.Fatalf("expected key to be extended on expiry")
	}

	clock = clock.Add(4 * time.Second)
	if store.Get("k1", &got) {
		t.Fatalf("expected key to expire after extension")
	}
}

func TestStoreStopPreservesEntries(t *testing.T) {
	store := NewStore[string, testValue]()

	if _, err := store.Set("k1", testValue{Packets: ptr(1)}); err != nil {
		t.Fatalf("set: %v", err)
	}

	store.Start(time.Millisecond)
	store.Stop()

	var got testValue
	if !store.Get("k1", &got) {
		t.Fatal("expected key to remain after Stop")
	}

	store.Close()
	if store.Get("k1", &got) {
		t.Fatal("expected key to be removed after Close")
	}
}
