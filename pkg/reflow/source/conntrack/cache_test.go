package conntrack

import (
	"net/netip"
	"testing"
	"time"
)

func fixtureEntry() Entry {
	return Entry{Original: Tuple{Src: netip.MustParseAddr("192.168.1.20"), Dst: netip.MustParseAddr("198.51.100.20"), SrcPort: 51000, DstPort: 443, Protocol: 6}, Reply: Tuple{Src: netip.MustParseAddr("198.51.100.20"), Dst: netip.MustParseAddr("203.0.113.9"), SrcPort: 443, DstPort: 62000, Protocol: 6}, ID: 42, Expires: time.Now().Add(time.Minute)}
}

func TestCacheMatchesBothSidesOfNAT(t *testing.T) {
	e := fixtureEntry()
	c := NewCache(8)
	c.Upsert(e)
	for _, test := range []struct {
		name             string
		tuple, pre, post Tuple
		reply            bool
	}{
		{"lan outgoing", e.Original, e.Original, e.Reply.Reverse(), false},
		{"wan outgoing", e.Reply.Reverse(), e.Original, e.Reply.Reverse(), false},
		{"wan reply", e.Reply, e.Reply, e.Original.Reverse(), true},
		{"lan reply", e.Original.Reverse(), e.Reply, e.Original.Reverse(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, ok := c.Lookup(test.tuple)
			if !ok || m.PreNAT() != test.pre || m.PostNAT() != test.post || m.ReplyDirection != test.reply {
				t.Fatalf("incorrect NAT match: %+v ok=%v", m, ok)
			}
		})
	}
	// A snapshot retains live updates while removing entries absent from the dump.
	stale := e
	stale.Original.SrcPort++
	stale.Reply.DstPort++
	c.Upsert(stale)
	c.BeginSnapshot()
	c.Upsert(e)
	c.EndSnapshot()
	if _, ok := c.Lookup(stale.Original); ok {
		t.Fatal("snapshot retained stale connection")
	}
	// A late destroy from the previous lifetime cannot evict a replacement,
	// including while reconciling a snapshot of the table.
	replacement := e
	replacement.ID++
	c.Upsert(replacement)
	c.BeginSnapshot()
	c.DeleteEntry(e)
	c.EndSnapshot()
	if m, ok := c.Lookup(e.Original); !ok || m.Entry.ID != replacement.ID {
		t.Fatal("late destroy removed replacement connection")
	}
	c.Delete(e.Original)
	if _, ok := c.Lookup(e.Reply); ok {
		t.Fatal("destroy did not remove translated aliases")
	}
}
