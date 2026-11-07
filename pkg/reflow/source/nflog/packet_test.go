package nflog

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/source/netlinkcapture"
)

func TestFirewallPacketCopies(t *testing.T) {
	for _, fixture := range []struct {
		name     string
		family   byte
		protocol uint16
		packet   string
	}{
		{"IPv4", 2, 0x800, "4500001c0000000040110000c0000201c63364013039003500080000"},
		{"IPv6", 10, 0x86dd, "600000000008114020010db800000000000000000000000120010db80000000000000000000000023039003500080000"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			packet, err := hex.DecodeString(fixture.packet)
			if err != nil {
				t.Fatal(err)
			}
			data := []byte{fixture.family, 0, 0, 42}
			header := make([]byte, 4)
			binary.BigEndian.PutUint16(header, fixture.protocol)
			header[2] = 2
			data = append(data, netlinkcapture.Attribute(1, header)...)
			data = append(data, netlinkcapture.Attribute(9, packet)...)
			data = append(data, netlinkcapture.Attribute(10, []byte("forward\x00"))...)
			index := make([]byte, 4)
			binary.BigEndian.PutUint32(index, 7)
			data = append(data, netlinkcapture.Attribute(4, index)...)
			stamp := make([]byte, 16)
			binary.BigEndian.PutUint64(stamp, 1700000000)
			binary.BigEndian.PutUint64(stamp[8:], 123456)
			data = append(data, netlinkcapture.Attribute(3, stamp)...)
			f, err := decodePacket(data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(f.Data, packet) || f.LinkType != 101 || f.CaptureLength != len(packet) || f.WireLength != 0 {
				t.Fatalf("packet copy changed: %+v", f)
			}
			if f.InterfaceIndex != 7 || f.Fields["nflog_hook"] != byte(2) || f.Fields["nflog_prefix"] != "forward" || f.Fields["nflog_group"] != uint16(42) {
				t.Fatalf("missing firewall metadata: %+v", f)
			}
			if !f.Timestamp.Equal(time.Unix(1700000000, 123456000)) {
				t.Fatalf("timestamp %s", f.Timestamp)
			}
		})
	}
}
