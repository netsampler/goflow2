package processor

import (
	"fmt"
	"net/netip"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/conntrack"
)

// ConntrackInput is implemented by an independent conntrack source. Wiring is
// scoped to an App instance, never a process-global registry.
type ConntrackInput interface {
	Name() string
	Cache() *conntrack.Cache
	Zone() uint16
}

// BindConntrackInput connects the requested input before processor workers run.
func (p *Builtin) BindConntrackInput(input ConntrackInput) error {
	if p.cfg.Conntrack.Input == "" {
		return nil
	}
	if input.Name() != p.cfg.Conntrack.Input {
		return fmt.Errorf("conntrack input name %q does not match %q", input.Name(), p.cfg.Conntrack.Input)
	}
	view := p.cfg.Conntrack.TupleView
	if view != "" && view != "observed" && view != "pre_nat" {
		return fmt.Errorf("conntrack tuple_view must be observed or pre_nat")
	}
	p.conntrack = input.Cache()
	p.conntrackZone = input.Zone()
	return nil
}

func (p *Builtin) enrichConntrack(evt *event.Event) {
	if p.conntrack == nil || evt.Fields == nil {
		return
	}
	if len(p.cfg.Conntrack.Networks) == 0 {
		if !config.IsCaptureNetwork(evt.Source.Network) {
			return
		}
	} else {
		found := false
		for _, network := range p.cfg.Conntrack.Networks {
			if evt.Source.Network == network {
				found = true
				break
			}
		}
		if !found {
			return
		}
	}
	fields := evt.Fields
	src, err := netip.ParseAddr(stringAlias(fields, "src_addr"))
	if err != nil || !src.Is4() {
		return
	}
	dst, err := netip.ParseAddr(stringAlias(fields, "dst_addr"))
	if err != nil || !dst.Is4() {
		return
	}
	proto := fieldUint32(fields, "proto")
	if proto != 6 && proto != 17 {
		return
	}
	tuple := conntrack.Tuple{Src: src, Dst: dst, SrcPort: uint16(fieldUint32(fields, "src_port")), DstPort: uint16(fieldUint32(fields, "dst_port")), Protocol: uint8(proto), Zone: p.conntrackZone}
	match, ok := p.conntrack.Lookup(tuple)
	fields["conntrack_match"] = ok
	if !ok {
		return
	}
	pre, post := match.PreNAT(), match.PostNAT()
	fields["conntrack_id"] = match.Entry.ID
	fields["conntrack_zone"] = p.conntrackZone
	fields["conntrack_original_src_addr"] = match.Entry.Original.Src.String()
	fields["conntrack_original_dst_addr"] = match.Entry.Original.Dst.String()
	fields["conntrack_original_src_port"] = uint32(match.Entry.Original.SrcPort)
	fields["conntrack_original_dst_port"] = uint32(match.Entry.Original.DstPort)
	fields["conntrack_direction"] = "original"
	if match.ReplyDirection {
		fields["conntrack_direction"] = "reply"
	}
	if pre.Src != post.Src || pre.SrcPort != post.SrcPort {
		fields["nat_src_addr"] = post.Src.String()
		fields["nat_src_port"] = uint32(post.SrcPort)
	}
	if pre.Dst != post.Dst || pre.DstPort != post.DstPort {
		fields["nat_dst_addr"] = post.Dst.String()
		fields["nat_dst_port"] = uint32(post.DstPort)
	}
	if p.cfg.Conntrack.TupleView == "pre_nat" {
		fields["observed_src_addr"] = src.String()
		fields["observed_dst_addr"] = dst.String()
		fields["observed_src_port"] = uint32(tuple.SrcPort)
		fields["observed_dst_port"] = uint32(tuple.DstPort)
		fields["src_addr"] = pre.Src.String()
		fields["dst_addr"] = pre.Dst.String()
		fields["src_port"] = uint32(pre.SrcPort)
		fields["dst_port"] = uint32(pre.DstPort)
	}
}
