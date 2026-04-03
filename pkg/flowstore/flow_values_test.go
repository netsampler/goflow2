package flowstore

import (
	"testing"
	"time"
)

type testKey = FlowIPv4Key

type testCounters struct {
	FlowCounters
	FlowTimestamp
	SrcAS uint32
	DstAS uint32
}

// Map IPv4 addresses to ASNs to simulate an external lookup used by the hook.
var ipToASN = map[FlowIPv4Addr]uint32{
	{192, 0, 2, 1}:    65010, // 192.0.2.1
	{198, 51, 100, 2}: 65020, // 198.51.100.2
}

func (c *testCounters) Add(delta testCounters, existed bool) error {
	if err := c.FlowCounters.Add(delta.FlowCounters, existed); err != nil {
		return err
	}
	if err := c.FlowTimestamp.Add(delta.FlowTimestamp, existed); err != nil {
		return err
	}
	return nil
}

func (c *testCounters) Set(val testCounters, existed bool) error {
	if err := c.FlowCounters.Set(val.FlowCounters, existed); err != nil {
		return err
	}
	if err := c.FlowTimestamp.Set(val.FlowTimestamp, existed); err != nil {
		return err
	}
	return nil
}

func (c *testCounters) CopyFrom(src testCounters) {
	_ = c.FlowCounters.Set(src.FlowCounters, true)
	c.FlowTimestamp.CopyFrom(src.FlowTimestamp)
	c.SrcAS = src.SrcAS
	c.DstAS = src.DstAS
}

// Interface guards are compile-time assertions that testCounters satisfies the
// FlowStore interfaces exercised by these tests.
var (
	_ Addable[testCounters]  = (*testCounters)(nil)
	_ Settable[testCounters] = (*testCounters)(nil)
	_ Copyable[testCounters] = (*testCounters)(nil)
)

func pInt64(v int64) *int64 { return &v }

func TestFlowCountersWithASHook(t *testing.T) {
	// Store with a mutate hook that sets ASN only on insert (not on update).
	store := NewStore[testKey, testCounters](WithHooks[testKey, testCounters](Hooks[testKey, testCounters]{
		OnSetMutate: func(key testKey, val *testCounters, existed bool) {
			if existed {
				return
			}
			val.SrcAS = ipToASN[key.Src]
			val.DstAS = ipToASN[key.Dst]
		},
	}))
	key := testKey{
		Src: FlowIPv4Addr{192, 0, 2, 1},
		Dst: FlowIPv4Addr{198, 51, 100, 2},
	}

	// Seed initial counters and timestamps via Set (should set ASNs via hook).
	initial := testCounters{
		FlowCounters: FlowCounters{
			Bytes:   pInt64(10),
			Packets: pInt64(1),
		},
		FlowTimestamp: FlowTimestamp{
			Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2024, 1, 1, 0, 0, 10, 0, time.UTC),
		},
	}
	if _, err := store.Set(key, initial); err != nil {
		t.Fatalf("set initial: %v", err)
	}
	t.Logf("after Set: bytes=%d packets=%d srcAS=%d dstAS=%d", deref64(initial.Bytes), deref64(initial.Packets), initial.SrcAS, initial.DstAS)

	var got testCounters
	if ok := store.Get(key, &got); !ok {
		t.Fatalf("expected value after initial set")
	}
	if got.SrcAS != ipToASN[key.Src] || got.DstAS != ipToASN[key.Dst] {
		t.Fatalf("unexpected AS after set: src %d dst %d", got.SrcAS, got.DstAS)
	}
	if got.Bytes == nil || got.Packets == nil || *got.Bytes != 10 || *got.Packets != 1 {
		t.Fatalf("unexpected counters after set: %+v", got)
	}
	if !got.Start.Equal(initial.Start) || !got.End.Equal(initial.End) {
		t.Fatalf("unexpected time window after set: start %v end %v", got.Start, got.End)
	}
	t.Logf("after Get initial: bytes=%d packets=%d srcAS=%d dstAS=%d", deref64(got.Bytes), deref64(got.Packets), got.SrcAS, got.DstAS)

	// Apply a delta via Add; ASNs must remain unchanged, end timestamp must advance.
	delta := testCounters{
		FlowCounters: FlowCounters{
			Bytes:   pInt64(5),
			Packets: pInt64(2),
		},
		FlowTimestamp: FlowTimestamp{
			Start: time.Date(2024, 1, 1, 0, 0, 10, 0, time.UTC), // wrong on purpose, should not be updated in the final set
			End:   time.Date(2024, 1, 1, 0, 0, 20, 0, time.UTC),
		},
	}
	if err := store.Add(key, delta); err != nil {
		t.Fatalf("add delta: %v", err)
	}

	if ok := store.Get(key, &got); !ok {
		t.Fatalf("expected value after add")
	}
	t.Logf("after Add: bytes=%d packets=%d srcAS=%d dstAS=%d", deref64(got.Bytes), deref64(got.Packets), got.SrcAS, got.DstAS)
	if got.SrcAS != ipToASN[key.Src] || got.DstAS != ipToASN[key.Dst] {
		t.Fatalf("expected AS to be preserved after add, got src %d dst %d", got.SrcAS, got.DstAS)
	}
	if got.Bytes == nil || got.Packets == nil || *got.Bytes != 15 || *got.Packets != 3 {
		t.Fatalf("unexpected counters after add: %+v", got)
	}
	if !got.Start.Equal(initial.Start) || !got.End.Equal(delta.End) {
		t.Fatalf("unexpected time window after add: start %v end %v", got.Start, got.End)
	}
}

func deref64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
