package nflog

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/netlinkcapture"
)

// decodePacket decodes the Netfilter envelope, never the captured packet itself.
func decodePacket(data []byte) (capture.Frame, error) {
	if len(data) < 4 {
		return capture.Frame{}, fmt.Errorf("short NFLOG message")
	}
	attrs, err := netlinkcapture.Attributes(data[4:])
	if err != nil {
		return capture.Frame{}, err
	}
	f := capture.Frame{Data: attrs[9], LinkType: 101, Fields: map[string]any{"nflog_family": data[0], "nflog_group": binary.BigEndian.Uint16(data[2:])}}
	if h := attrs[1]; len(h) >= 3 {
		f.Fields["nflog_protocol"] = binary.BigEndian.Uint16(h)
		f.Fields["nflog_hook"] = h[2]
	}
	for _, item := range []struct {
		attr  uint16
		field string
	}{{2, "firewall_mark"}, {4, "input_if"}, {5, "output_if"}, {6, "nflog_physical_input_if"}, {7, "nflog_physical_output_if"}, {11, "nflog_uid"}, {12, "nflog_sequence"}, {13, "nflog_global_sequence"}, {14, "nflog_gid"}} {
		if value := attrs[item.attr]; len(value) >= 4 {
			f.Fields[item.field] = binary.BigEndian.Uint32(value)
		}
	}
	if value := attrs[4]; len(value) >= 4 {
		f.InterfaceIndex = int(binary.BigEndian.Uint32(value))
	}
	if value := attrs[5]; len(value) >= 4 && f.InterfaceIndex == 0 {
		f.InterfaceIndex = int(binary.BigEndian.Uint32(value))
	}
	if value := attrs[3]; len(value) >= 16 {
		f.Timestamp = time.Unix(int64(binary.BigEndian.Uint64(value)), int64(binary.BigEndian.Uint64(value[8:]))*1000).UTC()
	}
	if value := attrs[10]; len(value) > 0 {
		if value[len(value)-1] == 0 {
			value = value[:len(value)-1]
		}
		f.Fields["nflog_prefix"] = string(value)
	}
	if l2 := attrs[21]; len(l2) > 0 {
		f.Data = append(append([]byte(nil), l2...), f.Data...)
		f.LinkType = 1
	}
	f.CaptureLength = len(f.Data)
	return f, nil
}
