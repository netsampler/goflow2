//go:build linux && !reflow_pcap && !reflow_dpdk

package conntrack

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/netlinkcapture"
	"golang.org/x/sys/unix"
)

// Source owns the event subscription, initial dump, and bounded lookup cache.
type Source struct {
	cfg   config.SourceConfig
	conn  *netlinkcapture.Conn
	cache *Cache
}

func New(cfg config.SourceConfig) (*Source, error) {
	if err := config.NormalizeConntrackSource(&cfg); err != nil {
		return nil, err
	}
	s := &Source{cfg: cfg, conn: netlinkcapture.NewConn(), cache: NewCache(cfg.Conntrack.MaxEntries)}
	if err := s.conn.Open(unix.NETLINK_NETFILTER, cfg.Conntrack.ReceiveBuffer); err != nil {
		return nil, fmt.Errorf("open conntrack netlink: %w", err)
	}
	for _, group := range []uint32{1, 2, 3} {
		if err := s.conn.Join(group); err != nil {
			_ = s.Close()
			return nil, fmt.Errorf("subscribe conntrack group %d (requires CAP_NET_ADMIN): %w", group, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.snapshot(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("initial conntrack dump: %w", err)
	}
	return s, nil
}

func (s *Source) Name() string  { return s.cfg.Conntrack.Name }
func (s *Source) Cache() *Cache { return s.cache }
func (s *Source) Zone() uint16  { return s.cfg.Conntrack.Zone }
func (s *Source) Close() error  { return s.conn.Close() }

func (s *Source) metadata() event.SourceMetadata {
	m := event.SourceMetadata{Network: "conntrack", Address: s.Name(), Type: "conntrack"}
	if s.cfg.SourceID != nil {
		m.SourceID = *s.cfg.SourceID
		m.SourceIDSet = true
	}
	return m
}
func (s *Source) InitEvents() ([]*event.Event, error) {
	return []*event.Event{{ReceivedAt: time.Now().UTC(), Kind: "control", Source: s.metadata(), Control: &event.ControlMetadata{Type: "conntrack_health"}, Fields: s.cache.Stats()}}, nil
}

// Start never performs work on a packet reader. Snapshot reconciliation and
// multicast updates share this reader, so no global state or external daemon is needed.
func (s *Source) Start(ctx context.Context, emit func(*event.Event) error) error {
	next := time.Now().Add(time.Duration(s.cfg.Conntrack.ResyncIntervalMS) * time.Millisecond)
	for ctx.Err() == nil {
		readCtx, cancel := context.WithDeadline(ctx, next)
		messages, err := s.conn.Read(readCtx)
		cancel()
		if errors.Is(err, context.DeadlineExceeded) {
			if err := s.snapshot(ctx); err != nil {
				return err
			}
			next = time.Now().Add(time.Duration(s.cfg.Conntrack.ResyncIntervalMS) * time.Millisecond)
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// On receive loss, invalidate stale mappings immediately and rebuild.
			if errors.Is(err, unix.ENOBUFS) {
				s.cache.Clear()
				slog.Warn("conntrack events lost; rebuilding cache", "input", s.Name())
				if err := s.snapshot(ctx); err != nil {
					return err
				}
				continue
			}
			return err
		}
		for _, msg := range messages {
			e, deleted, ok, err := s.apply(msg)
			if err != nil {
				return err
			}
			if ok && s.cfg.Conntrack.EmitEvents {
				fields := tupleFields(e.Original)
				fields["record_kind"] = "conntrack"
				fields["conntrack_id"] = e.ID
				fields["conntrack_deleted"] = deleted
				fields["conntrack_reply_src_addr"] = e.Reply.Src.String()
				fields["conntrack_reply_dst_addr"] = e.Reply.Dst.String()
				fields["conntrack_reply_src_port"] = e.Reply.SrcPort
				fields["conntrack_reply_dst_port"] = e.Reply.DstPort
				if err := emit(&event.Event{ReceivedAt: time.Now().UTC(), Kind: "conntrack", Source: s.metadata(), Fields: fields}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func tupleFields(t Tuple) map[string]any {
	return map[string]any{"src_addr": t.Src.String(), "dst_addr": t.Dst.String(), "src_port": t.SrcPort, "dst_port": t.DstPort, "proto": t.Protocol, "conntrack_zone": t.Zone}
}

func (s *Source) snapshot(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	seq, err := s.conn.Send(0x101, unix.NLM_F_REQUEST|unix.NLM_F_DUMP, []byte{unix.AF_INET, 0, 0, 0})
	if err != nil {
		return err
	}
	s.cache.BeginSnapshot()
	type update struct {
		id      uint32
		deleted bool
	}
	updates := make(map[Tuple]update)
	for {
		messages, err := s.conn.Read(ctx)
		if err != nil {
			s.cache.Clear()
			return fmt.Errorf("conntrack dump: %w", err)
		}
		done := false
		for _, msg := range messages {
			if msg.Sequence == seq && msg.Type == unix.NLMSG_DONE {
				if msg.Flags&unix.NLM_F_DUMP_INTR != 0 {
					s.cache.Clear()
					return fmt.Errorf("conntrack dump interrupted")
				}
				done = true
				continue
			}
			if err := messageError(msg); err != nil {
				s.cache.Clear()
				return err
			}
			e, deleted, ok, err := parseEntry(msg, s.cfg.Conntrack.Zone)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			if previous, ok := updates[e.Original]; msg.Sequence == seq && ok {
				// A destroy for an older connection must not suppress a dump
				// of its replacement using the same tuple and a new ID.
				if !previous.deleted || previous.id == 0 || previous.id == e.ID {
					continue
				}
			}
			if msg.Sequence != seq {
				updates[e.Original] = update{id: e.ID, deleted: deleted}
			}
			if deleted {
				s.cache.DeleteEntry(e)
			} else {
				s.cache.Upsert(e)
			}
		}
		if done {
			s.cache.EndSnapshot()
			return nil
		}
	}
}

func (s *Source) apply(msg netlinkcapture.Message) (Entry, bool, bool, error) {
	if err := messageError(msg); err != nil {
		return Entry{}, false, false, err
	}
	e, deleted, ok, err := parseEntry(msg, s.cfg.Conntrack.Zone)
	if ok && err == nil {
		if deleted {
			s.cache.DeleteEntry(e)
		} else {
			s.cache.Upsert(e)
		}
	}
	return e, deleted, ok, err
}
func messageError(msg netlinkcapture.Message) error {
	if msg.Type == unix.NLMSG_ERROR && len(msg.Data) >= 4 {
		if code := int32(binary.NativeEndian.Uint32(msg.Data)); code != 0 {
			return unix.Errno(-code)
		}
	}
	return nil
}

func parseEntry(msg netlinkcapture.Message, zone uint16) (Entry, bool, bool, error) {
	if msg.Type>>8 != 1 || len(msg.Data) < 4 || msg.Data[0] != unix.AF_INET {
		return Entry{}, false, false, nil
	}
	attrs, err := netlinkcapture.Attributes(msg.Data[4:])
	if err != nil {
		return Entry{}, false, false, err
	}
	var e Entry
	if v := attrs[18]; len(v) >= 2 {
		e.Original.Zone = binary.BigEndian.Uint16(v)
	}
	if e.Original.Zone != zone {
		return e, false, false, nil
	}
	e.Original, err = parseTuple(attrs[1], zone)
	if err != nil {
		return e, false, false, err
	}
	if e.Original.Protocol != 6 && e.Original.Protocol != 17 {
		return e, false, false, nil
	}
	deleted := msg.Type&0xff == 2
	if len(attrs[2]) != 0 {
		e.Reply, err = parseTuple(attrs[2], zone)
		if err != nil {
			return e, deleted, false, err
		}
	} else if !deleted {
		return e, deleted, false, nil
	}
	if v := attrs[12]; len(v) >= 4 {
		e.ID = binary.BigEndian.Uint32(v)
	}
	if v := attrs[7]; len(v) >= 4 {
		e.Expires = time.Now().Add(time.Duration(binary.BigEndian.Uint32(v)) * time.Second)
	}
	return e, deleted, true, nil
}

func parseTuple(data []byte, zone uint16) (Tuple, error) {
	var t Tuple
	t.Zone = zone
	attrs, err := netlinkcapture.Attributes(data)
	if err != nil {
		return t, err
	}
	ip, err := netlinkcapture.Attributes(attrs[1])
	if err != nil {
		return t, err
	}
	if len(ip[1]) != 4 || len(ip[2]) != 4 {
		return t, fmt.Errorf("conntrack IPv4 tuple is incomplete")
	}
	t.Src = netip.AddrFrom4([4]byte(ip[1]))
	t.Dst = netip.AddrFrom4([4]byte(ip[2]))
	proto, err := netlinkcapture.Attributes(attrs[2])
	if err != nil {
		return t, err
	}
	if len(proto[1]) == 0 {
		return t, fmt.Errorf("conntrack tuple protocol is missing")
	}
	t.Protocol = proto[1][0]
	if v := proto[2]; len(v) >= 2 {
		t.SrcPort = binary.BigEndian.Uint16(v)
	}
	if v := proto[3]; len(v) >= 2 {
		t.DstPort = binary.BigEndian.Uint16(v)
	}
	return t, nil
}
