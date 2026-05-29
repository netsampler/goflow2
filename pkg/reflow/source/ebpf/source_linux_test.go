//go:build linux && !reflow_noebpf

package ebpf

import (
	"encoding/binary"
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

func TestPacketMetadataMarksOutgoingCapture(t *testing.T) {
	source := &Source{
		cfg:                   config.SourceConfig{Network: "ebpf", Interface: "br-lan", Type: "bytes", SampleEvery: 1},
		agentIP:               "192.0.2.10",
		captureInterfaceIndex: 7,
		seenCount:             1,
	}

	meta := source.packetMetadata(&unix.SockaddrLinklayer{
		Ifindex: 7,
		Pkttype: packetOutgoing,
	})
	evt := source.packetEvent([]byte{0, 1, 2, 3}, meta)

	if evt.Source.CaptureDirection != "out" {
		t.Fatalf("expected source capture direction out, got %q", evt.Source.CaptureDirection)
	}
	if evt.Source.CapturePacketType != "outgoing" {
		t.Fatalf("expected packet type outgoing, got %q", evt.Source.CapturePacketType)
	}
	if got := evt.Fields["output_if"]; got != uint32(7) {
		t.Fatalf("expected output_if=7, got %#v", got)
	}
	if _, ok := evt.Fields["input_if"]; ok {
		t.Fatalf("expected input_if to be unset for outgoing packet, got %#v", evt.Fields["input_if"])
	}
	if got := evt.Fields["output_interface"]; got != "br-lan" {
		t.Fatalf("expected output_interface=br-lan, got %#v", got)
	}
	if _, ok := evt.Fields["dst_interface"]; ok {
		t.Fatalf("expected dst_interface alias to be unset, got %#v", evt.Fields["dst_interface"])
	}
	for _, key := range []string{"agent_ip", "sampling_rate", "sample_pool", "drops"} {
		if _, ok := evt.Fields[key]; ok {
			t.Fatalf("expected %s to stay in source metadata, got field value %#v", key, evt.Fields[key])
		}
	}
}

func TestParsePerfPacketSampleCombinesMetadataAndPacket(t *testing.T) {
	packet := []byte{0xde, 0xad, 0xbe, 0xef}
	sample := make([]byte, testSKBMetadataSize()+len(packet))
	binary.LittleEndian.PutUint32(sample[0:4], 1514)
	binary.LittleEndian.PutUint32(sample[4:8], packetOutgoing)
	binary.LittleEndian.PutUint32(sample[8:12], 42)
	binary.LittleEndian.PutUint32(sample[12:16], 3)
	binary.LittleEndian.PutUint32(sample[16:20], 0x0800)
	binary.LittleEndian.PutUint32(sample[20:24], 5)
	binary.LittleEndian.PutUint32(sample[24:28], 7)
	binary.LittleEndian.PutUint32(sample[28:32], 9)
	binary.LittleEndian.PutUint32(sample[32:36], 11)
	binary.LittleEndian.PutUint32(sample[36:40], 0x12345678)
	binary.LittleEndian.PutUint32(sample[40:44], 0x10002)
	copy(sample[testSKBMetadataSize():], packet)

	meta, gotPacket, err := parsePerfPacketSample(sample)
	if err != nil {
		t.Fatalf("parse perf packet sample: %v", err)
	}
	if meta.Len != 1514 || meta.PacketType != packetOutgoing || meta.Mark != 42 || meta.Hash != 0x12345678 {
		t.Fatalf("unexpected metadata: %#v", meta)
	}
	if string(gotPacket) != string(packet) {
		t.Fatalf("expected packet %x, got %x", packet, gotPacket)
	}
	gotPacket[0] = 0
	if packet[0] == 0 {
		t.Fatalf("expected parsed packet to be copied away from the sample buffer")
	}
}

func TestPacketMetadataFromSKBEventPreservesBranchAliases(t *testing.T) {
	source := &Source{
		cfg:                   config.SourceConfig{Network: "ebpf", Interface: "br-lan", Type: "bytes", SampleEvery: 1},
		agentIP:               "192.0.2.10",
		captureInterfaceIndex: 7,
		interfaceNames: map[uint32]string{
			7: "br-lan",
			9: "wan",
		},
	}
	meta := source.packetMetadataFromSKB(skbMetadata{
		Len:            1514,
		PacketType:     packetOutgoing,
		Mark:           42,
		IngressIfindex: 7,
		Ifindex:        9,
	})
	if meta.direction != "out" || meta.packetType != "outgoing" {
		t.Fatalf("unexpected packet direction/type: %#v", meta)
	}
	if meta.outputIf != 9 || meta.outputInterface != "wan" {
		t.Fatalf("expected output interface from skb ifindex, got %#v", meta)
	}
	if !meta.hasSKBMetadata {
		t.Fatalf("expected skb metadata to be marked present")
	}
	evt := source.packetEvent([]byte{0, 1, 2, 3}, meta)
	if _, ok := evt.Fields["firewall_mark"]; ok {
		t.Fatalf("expected firewall_mark alias to be unset, got %#v", evt.Fields["firewall_mark"])
	}
	if _, ok := evt.Fields["dst_interface"]; ok {
		t.Fatalf("expected dst_interface alias to be unset, got %#v", evt.Fields["dst_interface"])
	}
}

func TestEmitPerfSampleAppliesDirectionBeforeSampling(t *testing.T) {
	source := &Source{
		cfg: config.SourceConfig{
			Network:     "ebpf",
			Interface:   "br-lan",
			Type:        "bytes",
			SampleEvery: 1,
			EBPF:        config.EBPFConfig{Direction: "egress"},
		},
		agentIP:               "192.0.2.10",
		captureInterfaceIndex: 7,
		interfaceNames: map[uint32]string{
			7: "br-lan",
		},
	}
	sample := make([]byte, testSKBMetadataSize()+4)
	binary.LittleEndian.PutUint32(sample[4:8], packetHost)
	binary.LittleEndian.PutUint32(sample[24:28], 7)
	if err := source.emitPerfSample(sample, func(*event.Event) error {
		t.Fatalf("did not expect ingress packet to be emitted")
		return nil
	}); err != nil {
		t.Fatalf("emit perf sample: %v", err)
	}
	if source.seenCount != 0 {
		t.Fatalf("expected filtered packet not to advance sampling pool, got %d", source.seenCount)
	}
}

func TestParseCPUList(t *testing.T) {
	got := parseCPUList("0-2,4,6-7")
	want := []int{0, 1, 2, 4, 6, 7}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
	if got := parseCPUList("2-1"); got != nil {
		t.Fatalf("expected invalid range to return nil, got %v", got)
	}
}

func testSKBMetadataSize() int {
	return 44
}

func TestConntrackLineExtractsNATMetadata(t *testing.T) {
	line := "ipv4 2 tcp 6 431999 ESTABLISHED src=192.168.1.10 dst=198.51.100.20 sport=12345 dport=443 packets=10 bytes=1000 src=198.51.100.20 dst=203.0.113.9 sport=443 dport=54321 packets=8 bytes=900 [ASSURED] mark=0 use=1"
	meta, ok := parseConntrackLine(line)
	if !ok {
		t.Fatalf("expected conntrack line to parse")
	}
	if !meta.hasSNAT {
		t.Fatalf("expected SNAT metadata: %#v", meta)
	}
	if meta.natSrc.String() != "203.0.113.9" || meta.natSPort != 54321 {
		t.Fatalf("unexpected SNAT endpoint %s:%d", meta.natSrc, meta.natSPort)
	}
	if meta.hasDNAT {
		t.Fatalf("did not expect DNAT metadata: %#v", meta)
	}
	if meta.state != "established" || meta.status != "assured" {
		t.Fatalf("unexpected state/status: %q/%q", meta.state, meta.status)
	}
}

func TestPacketConntrackTupleParsesIPv4TCP(t *testing.T) {
	frame := testIPv4TCPFrame("192.168.1.10", "198.51.100.20", 12345, 443)
	tuple, ok := packetConntrackTuple(frame)
	if !ok {
		t.Fatalf("expected packet tuple")
	}
	if tuple.family != "ipv4" || tuple.proto != "tcp" {
		t.Fatalf("unexpected tuple family/proto: %#v", tuple)
	}
	if tuple.src.String() != "192.168.1.10" || tuple.dst.String() != "198.51.100.20" {
		t.Fatalf("unexpected tuple addresses: %#v", tuple)
	}
	if tuple.srcPort != 12345 || tuple.dstPort != 443 {
		t.Fatalf("unexpected tuple ports: %#v", tuple)
	}
}

func TestPacketEventAddsConntrackFields(t *testing.T) {
	source := &Source{
		cfg:                   config.SourceConfig{Network: "ebpf", Interface: "br-lan", Type: "bytes", SampleEvery: 1},
		agentIP:               "192.0.2.10",
		captureInterfaceIndex: 7,
		seenCount:             1,
	}
	meta := packetMetadata{
		packetType:   "host",
		direction:    "in",
		hasConntrack: true,
		conntrack: conntrackMetadata{
			direction: "original",
			family:    "ipv4",
			proto:     "tcp",
			state:     "established",
			status:    "assured",
			original: conntrackTuple{
				family:  "ipv4",
				proto:   "tcp",
				src:     netip.MustParseAddr("192.168.1.10"),
				dst:     netip.MustParseAddr("198.51.100.20"),
				srcPort: 12345,
				dstPort: 443,
			},
			reply: conntrackTuple{
				family:  "ipv4",
				proto:   "tcp",
				src:     netip.MustParseAddr("198.51.100.20"),
				dst:     netip.MustParseAddr("203.0.113.9"),
				srcPort: 443,
				dstPort: 54321,
			},
			hasSNAT:  true,
			natSrc:   netip.MustParseAddr("203.0.113.9"),
			natSPort: 54321,
		},
	}

	evt := source.packetEvent([]byte{0, 1, 2, 3}, meta)
	if got := evt.Fields["conntrack_state"]; got != "established" {
		t.Fatalf("expected conntrack_state=established, got %#v", got)
	}
	if got := evt.Fields["conntrack_status"]; got != "assured" {
		t.Fatalf("expected conntrack_status=assured, got %#v", got)
	}
	if _, ok := evt.Fields["nat_src_addr"]; ok {
		t.Fatalf("expected source event not to derive nat_src_addr, got %#v", evt.Fields["nat_src_addr"])
	}
	if _, ok := evt.Fields["nat_src_port"]; ok {
		t.Fatalf("expected source event not to derive nat_src_port, got %#v", evt.Fields["nat_src_port"])
	}
	for _, key := range []string{"conntrack_reply_src_addr", "conntrack_reply_dst_addr", "conntrack_reply_src_port", "conntrack_reply_dst_port"} {
		if _, ok := evt.Fields[key]; ok {
			t.Fatalf("expected source event not to export %s, got %#v", key, evt.Fields[key])
		}
	}
	if got := evt.Internal["conntrack_reply_dst_addr"]; got != "203.0.113.9" {
		t.Fatalf("expected internal conntrack_reply_dst_addr, got %#v", got)
	}
	if got := evt.Internal["conntrack_reply_dst_port"]; got != uint32(54321) {
		t.Fatalf("expected internal conntrack_reply_dst_port=54321, got %#v", got)
	}
}

func testIPv4TCPFrame(src, dst string, sport, dport uint16) []byte {
	frame := make([]byte, 14+20+20)
	frame[12] = 0x08
	frame[13] = 0x00
	ip := frame[14:]
	ip[0] = 0x45
	ip[9] = 6
	srcAddr := netip.MustParseAddr(src).As4()
	dstAddr := netip.MustParseAddr(dst).As4()
	copy(ip[12:16], srcAddr[:])
	copy(ip[16:20], dstAddr[:])
	tcp := ip[20:]
	tcp[0] = byte(sport >> 8)
	tcp[1] = byte(sport)
	tcp[2] = byte(dport >> 8)
	tcp[3] = byte(dport)
	return frame
}

func TestPacketMetadataMarksIncomingCapture(t *testing.T) {
	source := &Source{
		cfg:                   config.SourceConfig{Network: "ebpf", Interface: "br-lan", Type: "bytes", SampleEvery: 1},
		agentIP:               "192.0.2.10",
		captureInterfaceIndex: 7,
		seenCount:             1,
	}

	meta := source.packetMetadata(&unix.SockaddrLinklayer{
		Ifindex: 7,
		Pkttype: packetHost,
	})
	evt := source.packetEvent([]byte{0, 1, 2, 3}, meta)

	if evt.Source.CaptureDirection != "in" {
		t.Fatalf("expected source capture direction in, got %q", evt.Source.CaptureDirection)
	}
	if evt.Source.CapturePacketType != "host" {
		t.Fatalf("expected packet type host, got %q", evt.Source.CapturePacketType)
	}
	if got := evt.Fields["input_if"]; got != uint32(7) {
		t.Fatalf("expected input_if=7, got %#v", got)
	}
	if _, ok := evt.Fields["output_if"]; ok {
		t.Fatalf("expected output_if to be unset for incoming packet, got %#v", evt.Fields["output_if"])
	}
	if got := evt.Fields["input_interface"]; got != "br-lan" {
		t.Fatalf("expected input_interface=br-lan, got %#v", got)
	}
	if _, ok := evt.Fields["src_interface"]; ok {
		t.Fatalf("expected src_interface alias to be unset, got %#v", evt.Fields["src_interface"])
	}
}

func TestAllowDirectionFiltersIngressAndEgress(t *testing.T) {
	tests := []struct {
		filter    string
		direction string
		want      bool
	}{
		{filter: "both", direction: "in", want: true},
		{filter: "both", direction: "out", want: true},
		{filter: "both", direction: "loopback", want: true},
		{filter: "ingress", direction: "in", want: true},
		{filter: "ingress", direction: "out", want: false},
		{filter: "egress", direction: "out", want: true},
		{filter: "egress", direction: "in", want: false},
		{filter: "egress", direction: "loopback", want: false},
	}
	for _, tt := range tests {
		if got := allowDirection(tt.filter, tt.direction); got != tt.want {
			t.Fatalf("allowDirection(%q, %q)=%t, want %t", tt.filter, tt.direction, got, tt.want)
		}
	}
}

func TestPacketMetadataAddsSKBFields(t *testing.T) {
	source := &Source{
		cfg:                   config.SourceConfig{Network: "ebpf", Interface: "br-lan", Type: "bytes", SampleEvery: 1},
		agentIP:               "192.0.2.10",
		captureInterfaceIndex: 7,
		interfaceNames: map[uint32]string{
			7: "br-lan",
			9: "wan",
		},
		seenCount: 1,
	}

	meta := source.packetMetadata(&unix.SockaddrLinklayer{
		Ifindex: 7,
		Pkttype: packetOutgoing,
	})
	meta = source.mergeSKBMetadata(meta, skbMetadata{
		Len:            1514,
		Mark:           42,
		QueueMapping:   3,
		Protocol:       0x0800,
		Priority:       5,
		IngressIfindex: 7,
		Ifindex:        9,
		TCIndex:        11,
		Hash:           0x12345678,
		TCClassID:      0x10002,
	})
	evt := source.packetEvent([]byte{0, 1, 2, 3}, meta)

	if got := evt.Fields["skb_mark"]; got != uint32(42) {
		t.Fatalf("expected skb_mark=42, got %#v", got)
	}
	if _, ok := evt.Fields["firewall_mark"]; ok {
		t.Fatalf("expected firewall_mark alias to be unset, got %#v", evt.Fields["firewall_mark"])
	}
	if got := evt.Fields["skb_len"]; got != uint32(1514) {
		t.Fatalf("expected skb_len=1514, got %#v", got)
	}
	if got := evt.Fields["capture_length"]; got != 4 {
		t.Fatalf("expected capture_length=4, got %#v", got)
	}
	if got := evt.Fields["wire_length"]; got != 1514 {
		t.Fatalf("expected wire_length=1514, got %#v", got)
	}
	if got := evt.Fields["skb_hash"]; got != uint32(0x12345678) {
		t.Fatalf("expected skb_hash=0x12345678, got %#v", got)
	}
	if got := evt.Fields["skb_ingress_ifindex"]; got != uint32(7) {
		t.Fatalf("expected skb_ingress_ifindex=7, got %#v", got)
	}
	if got := evt.Fields["skb_ifindex"]; got != uint32(9) {
		t.Fatalf("expected skb_ifindex=9, got %#v", got)
	}
	for _, key := range []string{"route_input_if", "route_output_if", "dst_interface"} {
		if _, ok := evt.Fields[key]; ok {
			t.Fatalf("expected %s alias to be unset, got %#v", key, evt.Fields[key])
		}
	}
	if got := evt.Fields["output_interface"]; got != "wan" {
		t.Fatalf("expected output_interface=wan, got %#v", got)
	}
}
