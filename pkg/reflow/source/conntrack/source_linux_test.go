//go:build linux && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package conntrack

import (
	"context"
	"encoding/binary"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"golang.org/x/sys/unix"
	"os"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/source/netlinkcapture"
)

func encodeTuple(t Tuple) []byte {
	var ip, proto []byte
	ip = append(ip, netlinkcapture.Attribute(1, t.Src.AsSlice())...)
	ip = append(ip, netlinkcapture.Attribute(2, t.Dst.AsSlice())...)
	proto = append(proto, netlinkcapture.Attribute(1, []byte{t.Protocol})...)
	port := make([]byte, 2)
	binary.BigEndian.PutUint16(port, t.SrcPort)
	proto = append(proto, netlinkcapture.Attribute(2, port)...)
	binary.BigEndian.PutUint16(port, t.DstPort)
	proto = append(proto, netlinkcapture.Attribute(3, port)...)
	return append(netlinkcapture.Attribute(1, ip), netlinkcapture.Attribute(2, proto)...)
}

func TestNetlinkNATEntryLifecycle(t *testing.T) {
	e := fixtureEntry()
	data := []byte{2, 0, 0, 0}
	data = append(data, netlinkcapture.Attribute(1, encodeTuple(e.Original))...)
	data = append(data, netlinkcapture.Attribute(2, encodeTuple(e.Reply))...)
	id := make([]byte, 4)
	binary.BigEndian.PutUint32(id, e.ID)
	data = append(data, netlinkcapture.Attribute(12, id)...)
	s := &Source{cache: NewCache(8)}
	_, deleted, ok, err := s.apply(netlinkcapture.Message{Type: 0x100, Data: data})
	if err != nil || !ok || deleted {
		t.Fatalf("new conntrack record: ok=%v deleted=%v err=%v", ok, deleted, err)
	}
	if match, ok := s.cache.Lookup(e.Reply.Reverse()); !ok || match.Entry.ID != 42 {
		t.Fatalf("translated tuple unavailable: %+v %v", match, ok)
	}
	_, deleted, ok, err = s.apply(netlinkcapture.Message{Type: 0x102, Data: data})
	if err != nil || !ok || !deleted {
		t.Fatalf("destroy conntrack record: ok=%v deleted=%v err=%v", ok, deleted, err)
	}
	if _, ok := s.cache.Lookup(e.Original); ok {
		t.Fatal("destroy retained mapping")
	}
}

// This integration test only runs in a disposable network namespace. It creates
// a synthetic UDP conntrack entry using Netlink and removes it before returning.
func TestKernelConntrackSnapshotAndDestroy(t *testing.T) {
	if os.Getenv("REFLOW_CONNTRACK_INTEGRATION") != "1" {
		t.Skip("requires isolated Linux network namespace and CAP_NET_ADMIN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	control := netlinkcapture.NewConn()
	if err := control.Open(unix.NETLINK_NETFILTER, 1048576); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = control.Close() }()
	// With nf_conntrack_events=2, a listener must exist when the entry is
	// created for the kernel to allocate its event extension.
	if err := control.Join(3); err != nil {
		t.Fatal(err)
	}
	e := fixtureEntry()
	e.Original.Protocol = 17
	e.Reply = e.Original.Reverse()
	payload := []byte{unix.AF_INET, 0, 0, 0}
	payload = append(payload, netlinkcapture.Attribute(1, encodeTuple(e.Original))...)
	payload = append(payload, netlinkcapture.Attribute(2, encodeTuple(e.Reply))...)
	timeout := make([]byte, 4)
	binary.BigEndian.PutUint32(timeout, 60)
	payload = append(payload, netlinkcapture.Attribute(7, timeout)...)
	ack := func(typ, flags uint16) {
		t.Helper()
		seq, err := control.Send(typ, unix.NLM_F_REQUEST|unix.NLM_F_ACK|flags, payload)
		if err != nil {
			t.Fatal(err)
		}
		for {
			messages, err := control.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, msg := range messages {
				if msg.Sequence == seq && msg.Type == unix.NLMSG_ERROR {
					if err := messageError(msg); err != nil {
						t.Fatal(err)
					}
					return
				}
			}
		}
	}
	ack(0x100, unix.NLM_F_CREATE|unix.NLM_F_EXCL)
	defer func() { _, _ = control.Send(0x102, unix.NLM_F_REQUEST, payload) }()
	s, err := New(config.SourceConfig{Network: "conntrack", Conntrack: config.ConntrackSourceConfig{Name: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, ok := s.cache.Lookup(e.Original); !ok {
		t.Fatal("initial kernel dump omitted inserted connection")
	}
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx, func(*event.Event) error { return nil }) }()
	ack(0x102, 0)
	for {
		if _, ok := s.cache.Lookup(e.Original); !ok {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("kernel destroy event did not remove cache entry")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
