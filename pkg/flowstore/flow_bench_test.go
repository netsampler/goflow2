package flowstore

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type benchCounters struct {
	FlowCounters
	FlowTimestamp
}

// benchDeref64 safely reads an optional int64 pointer.
func benchDeref64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

func (c *benchCounters) Add(delta benchCounters, existed bool) error {
	if err := c.FlowCounters.Add(delta.FlowCounters, existed); err != nil {
		return err
	}
	return c.FlowTimestamp.Add(delta.FlowTimestamp, existed)
}

func (c *benchCounters) Set(val benchCounters, existed bool) error {
	if err := c.FlowCounters.Set(val.FlowCounters, existed); err != nil {
		return err
	}
	return c.FlowTimestamp.Set(val.FlowTimestamp, existed)
}

func (c *benchCounters) CopyFrom(src benchCounters) {
	_ = c.FlowCounters.Set(src.FlowCounters, true)
	c.FlowTimestamp.CopyFrom(src.FlowTimestamp)
}

// Interface guards are compile-time assertions that the benchmark value keeps
// implementing the FlowStore contracts used by the benchmark store.
var (
	_ Addable[benchCounters]  = (*benchCounters)(nil)
	_ Settable[benchCounters] = (*benchCounters)(nil)
	_ Copyable[benchCounters] = (*benchCounters)(nil)
)

func benchPInt64(v int64) *int64 { return &v }

func newBenchStore() *Store[FlowIPv4Key, benchCounters] {
	return NewStore[FlowIPv4Key, benchCounters]()
}

func BenchmarkStoreAddSingleKey(b *testing.B) {
	// Single fixed key to isolate per-key Add cost.
	key := FlowIPv4Key{
		Src: FlowIPv4Addr{192, 0, 2, 1},
		Dst: FlowIPv4Addr{198, 51, 100, 2},
	}
	store := newBenchStore()

	// Seed initial state so subsequent Add is always an update.
	initial := benchCounters{
		FlowCounters: FlowCounters{
			Bytes:   benchPInt64(10),
			Packets: benchPInt64(1),
		},
		FlowTimestamp: FlowTimestamp{
			Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2024, 1, 1, 0, 0, 10, 0, time.UTC),
		},
	}
	if _, err := store.Set(key, initial); err != nil {
		b.Fatalf("set initial: %v", err)
	}

	// Delta applied each iteration.
	delta := benchCounters{
		FlowCounters: FlowCounters{
			Bytes:   benchPInt64(5),
			Packets: benchPInt64(2),
		},
		FlowTimestamp: FlowTimestamp{
			End: time.Date(2024, 1, 1, 0, 0, 20, 0, time.UTC),
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Add(key, delta); err != nil {
			b.Fatalf("add delta: %v", err)
		}
	}
	b.StopTimer()

	// Validate counters and timestamps to ensure correctness.
	var got benchCounters
	if ok := store.Get(key, &got); !ok {
		b.Fatalf("expected value after add")
	}
	if got.Bytes == nil || got.Packets == nil {
		b.Fatalf("missing counters after add")
	}
	if !got.Start.Equal(initial.Start) {
		b.Fatalf("unexpected start: %v", got.Start)
	}
	if !got.End.Equal(delta.End) {
		b.Fatalf("unexpected end: %v want %v", got.End, delta.End)
	}
	wantBytes := benchDeref64(initial.Bytes) + int64(b.N)*benchDeref64(delta.Bytes)
	wantPackets := benchDeref64(initial.Packets) + int64(b.N)*benchDeref64(delta.Packets)
	if benchDeref64(got.Bytes) != wantBytes || benchDeref64(got.Packets) != wantPackets {
		b.Fatalf("unexpected totals: bytes=%d packets=%d want bytes=%d packets=%d", benchDeref64(got.Bytes), benchDeref64(got.Packets), wantBytes, wantPackets)
	}
	b.Logf("n=%d after Add: bytes=%d packets=%d start=%v end=%v", b.N, benchDeref64(got.Bytes), benchDeref64(got.Packets), got.Start, got.End)
}

func BenchmarkStoreAddMultipleKeys(b *testing.B) {
	const numKeys = 128
	// Deterministic set of distinct keys to spread updates.
	keys := make([]FlowIPv4Key, 0, numKeys)
	for i := 0; i < numKeys; i++ {
		src := FlowIPv4Addr{10, 0, 0, byte(i)}
		dst := FlowIPv4Addr{192, 0, 2, byte(i)}
		key := FlowIPv4Key{Src: src, Dst: dst}
		keys = append(keys, key)
	}
	store := newBenchStore()

	// Seed each key once.
	initial := benchCounters{
		FlowCounters: FlowCounters{
			Bytes:   benchPInt64(1),
			Packets: benchPInt64(1),
		},
		FlowTimestamp: FlowTimestamp{
			Start: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			End:   time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC),
		},
	}
	for _, key := range keys {
		if _, err := store.Set(key, initial); err != nil {
			b.Fatalf("set initial for %v: %v", key, err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	workers := 8
	chunk := (b.N + workers - 1) / workers
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	// Worker applies adds for its chunk of iterations.
	runAdds := func(start, end int) {
		defer wg.Done()
		for i := start; i < end; i++ {
			key := keys[i%len(keys)]
			addEnd := time.Date(2024, 1, 1, 0, 0, int(i%1000), 0, time.UTC)
			delta := benchCounters{
				FlowCounters: FlowCounters{
					Bytes:   benchPInt64(1),
					Packets: benchPInt64(1),
				},
				FlowTimestamp: FlowTimestamp{
					End: addEnd,
				},
			}
			if err := store.Add(key, delta); err != nil {
				select {
				case errCh <- fmt.Errorf("add delta for %v: %w", key, err):
				default:
				}
				return
			}
		}
	}

	for w := 0; w < workers; w++ {
		start := w * chunk
		end := start + chunk
		if end > b.N {
			end = b.N
		}
		if start >= end {
			continue
		}
		wg.Add(1)
		go runAdds(start, end)
	}
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		b.Fatalf("%v", err)
	}
	b.StopTimer()

	// Precompute the expected end timestamp per key (max of all applied deltas).
	var totalBytes, totalPackets int64
	expectedEnd := make(map[FlowIPv4Key]time.Time, len(keys))
	for idx, key := range keys {
		maxSec := int(initial.End.Second())
		for i := idx; i < b.N; i += len(keys) {
			if sec := i % 1000; sec > maxSec {
				maxSec = sec
			}
		}
		expectedEnd[key] = time.Date(2024, 1, 1, 0, 0, maxSec, 0, time.UTC)
	}
	for _, key := range keys {
		var got benchCounters
		if ok := store.Get(key, &got); !ok {
			b.Fatalf("expected value for %v", key)
		}
		if !got.Start.Equal(initial.Start) {
			b.Fatalf("unexpected start for %v: %v", key, got.Start)
		}
		if wantEnd, ok := expectedEnd[key]; ok {
			if !got.End.Equal(wantEnd) {
				b.Fatalf("unexpected end for %v: %v want %v", key, got.End, wantEnd)
			}
		}
		totalBytes += benchDeref64(got.Bytes)
		totalPackets += benchDeref64(got.Packets)
	}
	wantBytes := int64(len(keys))*benchDeref64(initial.Bytes) + int64(b.N)*benchDeref64(benchPInt64(1))
	wantPackets := int64(len(keys))*benchDeref64(initial.Packets) + int64(b.N)*benchDeref64(benchPInt64(1))
	if totalBytes != wantBytes || totalPackets != wantPackets {
		b.Fatalf("unexpected totals across keys: bytes=%d packets=%d want bytes=%d packets=%d", totalBytes, totalPackets, wantBytes, wantPackets)
	}
	firstEnd := expectedEnd[keys[0]]
	b.Logf("n=%d after Add: bytes=%d packets=%d start=%v end=%v", b.N, totalBytes, totalPackets, initial.Start, firstEnd)
}
