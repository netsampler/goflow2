package processor

import (
	"net/netip"
	"testing"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/conntrack"
)

type testConntrackInput struct{ cache *conntrack.Cache }

func (i testConntrackInput) Name() string            { return "router" }
func (i testConntrackInput) Cache() *conntrack.Cache { return i.cache }
func (i testConntrackInput) Zone() uint16            { return 0 }

func TestSharedConntrackEnrichesAFPacketNAT(t *testing.T) {
	original := conntrack.Tuple{Src: netip.MustParseAddr("192.168.1.20"), Dst: netip.MustParseAddr("198.51.100.20"), SrcPort: 51000, DstPort: 443, Protocol: 6}
	reply := conntrack.Tuple{Src: original.Dst, Dst: netip.MustParseAddr("203.0.113.9"), SrcPort: 443, DstPort: 62000, Protocol: 6}
	cache := conntrack.NewCache(8)
	cache.Upsert(conntrack.Entry{Original: original, Reply: reply, ID: 42})
	p := NewBuiltin(config.ProcessorConfig{Builtin: config.BuiltinProcessorConfig{Conntrack: config.ConntrackProcessorConfig{Input: "router", TupleView: "pre_nat"}}})
	if err := p.BindConntrackInput(testConntrackInput{cache}); err != nil {
		t.Fatal(err)
	}
	for _, tuple := range []conntrack.Tuple{reply.Reverse(), reply} {
		// Exercise the normal bytes mapper with a captured Ethernet/IPv4/TCP header.
		frame := make([]byte, 54)
		frame[12] = 8
		frame[14] = 0x45
		frame[17] = 40
		frame[23] = 6
		copy(frame[26:30], tuple.Src.AsSlice())
		copy(frame[30:34], tuple.Dst.AsSlice())
		frame[34] = byte(tuple.SrcPort >> 8)
		frame[35] = byte(tuple.SrcPort)
		frame[36] = byte(tuple.DstPort >> 8)
		frame[37] = byte(tuple.DstPort)
		frame[46] = 0x50
		evt := &event.Event{Source: event.SourceMetadata{Network: "af_packet", Type: "bytes"}, Payload: frame}
		_, err := p.Process(evt)
		if err != nil {
			t.Fatal(err)
		}
		if evt.Fields["conntrack_match"] != true {
			t.Fatalf("no conntrack match: %+v", evt.Fields)
		}
		if tuple == reply.Reverse() {
			if evt.Fields["src_addr"] != "192.168.1.20" || evt.Fields["src_port"] != uint32(51000) || evt.Fields["nat_src_addr"] != "203.0.113.9" || evt.Fields["nat_src_port"] != uint32(62000) {
				t.Fatalf("outbound SNAT mapping: %+v", evt.Fields)
			}
		} else if evt.Fields["dst_addr"] != "203.0.113.9" || evt.Fields["dst_port"] != uint32(62000) || evt.Fields["nat_dst_addr"] != "192.168.1.20" || evt.Fields["nat_dst_port"] != uint32(51000) {
			t.Fatalf("reply SNAT mapping: %+v", evt.Fields)
		}
	}
	// A forwarded port (DNAT) has a different post-NAT destination and port.
	dnat := conntrack.Entry{Original: conntrack.Tuple{Src: netip.MustParseAddr("198.51.100.30"), Dst: netip.MustParseAddr("203.0.113.9"), SrcPort: 54000, DstPort: 8443, Protocol: 6}, Reply: conntrack.Tuple{Src: netip.MustParseAddr("192.168.1.30"), Dst: netip.MustParseAddr("198.51.100.30"), SrcPort: 443, DstPort: 54000, Protocol: 6}}
	cache.Upsert(dnat)
	evt := &event.Event{Source: event.SourceMetadata{Network: "af_packet"}, Fields: map[string]any{"src_addr": "198.51.100.30", "dst_addr": "192.168.1.30", "src_port": uint32(54000), "dst_port": uint32(443), "proto": uint32(6)}}
	p.enrichConntrack(evt)
	if evt.Fields["dst_addr"] != "203.0.113.9" || evt.Fields["dst_port"] != uint32(8443) || evt.Fields["nat_dst_addr"] != "192.168.1.30" || evt.Fields["nat_dst_port"] != uint32(443) {
		t.Fatalf("DNAT mapping: %+v", evt.Fields)
	}
}
