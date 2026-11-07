// Package conntrack provides a local connection-state input and a cache shared
// with packet processors. It does not perform packet capture or alter forwarding.
package conntrack

import (
	"container/list"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"
)

// Tuple is directional and includes the conntrack zone to prevent cross-table matches.
type Tuple struct {
	Src, Dst         netip.Addr
	SrcPort, DstPort uint16
	Protocol         uint8
	Zone             uint16
}

func (t Tuple) Reverse() Tuple {
	t.Src, t.Dst = t.Dst, t.Src
	t.SrcPort, t.DstPort = t.DstPort, t.SrcPort
	return t
}

type Entry struct {
	Original, Reply Tuple
	ID              uint32
	Expires         time.Time
}

type Match struct {
	Entry          Entry
	ReplyDirection bool
}

func (m Match) PreNAT() Tuple {
	if m.ReplyDirection {
		return m.Entry.Reply
	}
	return m.Entry.Original
}
func (m Match) PostNAT() Tuple {
	if m.ReplyDirection {
		return m.Entry.Original.Reverse()
	}
	return m.Entry.Reply.Reverse()
}

type cacheEntry struct {
	entry      Entry
	generation uint64
	element    *list.Element
}

type Cache struct {
	mu                      sync.RWMutex
	entries                 map[Tuple]*cacheEntry
	aliases                 map[Tuple]Match
	order                   *list.List
	limit                   int
	generation              uint64
	hits, misses, evictions atomic.Uint64
}

func NewCache(limit int) *Cache {
	return &Cache{entries: make(map[Tuple]*cacheEntry), aliases: make(map[Tuple]Match), order: list.New(), limit: max(1, limit)}
}

func aliases(e Entry) [4]Tuple {
	return [4]Tuple{e.Original, e.Reply.Reverse(), e.Reply, e.Original.Reverse()}
}

func (c *Cache) remove(key Tuple) {
	e := c.entries[key]
	if e == nil {
		return
	}
	for _, tuple := range aliases(e.entry) {
		if m, ok := c.aliases[tuple]; ok && m.Entry.Original == key {
			delete(c.aliases, tuple)
		}
	}
	c.order.Remove(e.element)
	delete(c.entries, key)
}

func (c *Cache) Upsert(e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remove(e.Original)
	if len(c.entries) >= c.limit {
		c.remove(c.order.Back().Value.(Tuple))
		c.evictions.Add(1)
	}
	item := &cacheEntry{entry: e, generation: c.generation, element: c.order.PushFront(e.Original)}
	c.entries[e.Original] = item
	for i, tuple := range aliases(e) {
		c.aliases[tuple] = Match{Entry: e, ReplyDirection: i >= 2}
	}
}

func (c *Cache) Delete(original Tuple) { c.mu.Lock(); defer c.mu.Unlock(); c.remove(original) }

// DeleteEntry ignores a delayed destroy for an older connection that reused
// the same tuple. The kernel connection ID distinguishes those lifetimes.
func (c *Cache) DeleteEntry(entry Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current := c.entries[entry.Original]; current != nil {
		if entry.ID == 0 || current.entry.ID == 0 || current.entry.ID == entry.ID {
			c.remove(entry.Original)
		} else {
			// A stale destroy also proves that the replacement must survive
			// this snapshot even if its dump record precedes that event.
			current.generation = c.generation
		}
	}
}

func (c *Cache) Lookup(tuple Tuple) (Match, bool) {
	c.mu.RLock()
	m, ok := c.aliases[tuple]
	c.mu.RUnlock()
	if ok && !m.Entry.Expires.IsZero() && time.Now().After(m.Entry.Expires) {
		ok = false
	}
	if ok {
		c.hits.Add(1)
	} else {
		c.misses.Add(1)
	}
	return m, ok
}

// BeginSnapshot and EndSnapshot reconcile missed destroy events. The source
// applies multicast updates and dump replies on a single reader during a dump.
func (c *Cache) BeginSnapshot() { c.mu.Lock(); defer c.mu.Unlock(); c.generation++ }
func (c *Cache) EndSnapshot() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, value := range c.entries {
		if value.generation != c.generation {
			c.remove(key)
		}
	}
}
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	clear(c.aliases)
	c.order.Init()
}

func (c *Cache) Stats() map[string]any {
	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	return map[string]any{"conntrack_entries": n, "conntrack_hits": c.hits.Load(), "conntrack_misses": c.misses.Load(), "conntrack_evictions": c.evictions.Load()}
}
