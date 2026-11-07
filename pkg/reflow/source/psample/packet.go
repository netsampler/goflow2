package psample

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"

	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/netlinkcapture"
)

// Attribute numbers follow include/uapi/linux/psample.h (starting at zero).
type decoder struct {
	groups   map[uint32]bool
	ports    map[string]string
	sequence map[uint32]uint32
	gaps     map[uint32]uint64
}

func newDecoder(groups []uint32, ports map[string]string) *decoder {
	d := &decoder{groups: map[uint32]bool{}, ports: ports, sequence: map[uint32]uint32{}, gaps: map[uint32]uint64{}}
	for _, group := range groups {
		d.groups[group] = true
	}
	return d
}

func (d *decoder) packet(data []byte) (capture.Frame, bool, error) {
	if len(data) < 4 {
		return capture.Frame{}, false, fmt.Errorf("short psample generic-netlink header")
	}
	if data[0] != 0 {
		return capture.Frame{}, false, nil
	}
	attrs, err := netlinkcapture.Attributes(data[4:])
	if err != nil {
		return capture.Frame{}, false, err
	}
	u32 := func(k uint16) uint32 {
		if v := attrs[k]; len(v) >= 4 {
			return binary.NativeEndian.Uint32(v)
		}
		return 0
	}
	group, seq := u32(3), u32(4)
	if len(d.groups) > 0 && !d.groups[group] {
		return capture.Frame{}, false, nil
	}
	if _, exists := attrs[6]; !exists {
		return capture.Frame{}, false, nil
	}
	if _, exists := attrs[4]; exists {
		if previous, ok := d.sequence[group]; ok {
			delta := seq - previous
			if delta > 1 && delta < 1<<31 {
				d.gaps[group] += uint64(delta - 1)
			}
		}
		d.sequence[group] = seq
	}
	f := capture.Frame{Data: attrs[6], LinkType: 1, WireLength: int(u32(2)), Fields: map[string]any{"psample_group": group, "psample_sequence": seq, "psample_sequence_gaps": d.gaps[group]}}
	f.CaptureLength = len(f.Data)
	for _, item := range []struct {
		attr uint16
		name string
	}{{0, "input_if"}, {1, "output_if"}} {
		v := attrs[item.attr]
		if len(v) == 0 {
			continue
		}
		var id uint64
		switch {
		case len(v) >= 8:
			id = binary.NativeEndian.Uint64(v)
		case len(v) >= 4:
			id = uint64(binary.NativeEndian.Uint32(v))
		case len(v) >= 2:
			id = uint64(binary.NativeEndian.Uint16(v))
		default:
			continue
		}
		f.Fields["psample_"+item.name] = id
		index, name, mapped := d.port(id)
		if name != "" {
			key := "input_interface"
			if item.attr == 1 {
				key = "output_interface"
			}
			f.Fields[key] = name
			if f.Interface == "" {
				f.Interface = name
			}
		}
		if mapped {
			f.Fields[item.name] = uint32(index)
			if f.InterfaceIndex == 0 {
				f.InterfaceIndex = index
				f.Interface = name
			}
		}
	}
	if len(attrs[0]) > 0 && len(attrs[1]) == 0 {
		f.Direction = "ingress"
	}
	if len(attrs[1]) > 0 && len(attrs[0]) == 0 {
		f.Direction = "egress"
	}
	if v := attrs[5]; len(v) >= 4 {
		rate := u32(5)
		f.Fields["psample_sample_rate"] = rate
		if _, probability := attrs[16]; probability {
			f.Fields["psample_sample_probability"] = float64(rate) / float64(^uint32(0))
		} else {
			f.Sampling = &event.SamplingMetadata{Rate: rate}
		}
	}
	if v := attrs[10]; len(v) >= 2 {
		f.Fields["psample_output_queue"] = binary.NativeEndian.Uint16(v)
	}
	for _, item := range []struct {
		attr uint16
		name string
	}{{11, "psample_queue_occupancy_bytes"}, {12, "psample_latency_ns"}, {13, "psample_timestamp_ns"}} {
		if v := attrs[item.attr]; len(v) >= 8 {
			f.Fields[item.name] = binary.NativeEndian.Uint64(v)
		}
	}
	if v := attrs[14]; len(v) >= 2 {
		proto := binary.NativeEndian.Uint16(v)
		f.Fields["psample_protocol"] = proto
		if len(f.Data) > 0 && ((proto == 0x0800 && f.Data[0]>>4 == 4) || (proto == 0x86dd && f.Data[0]>>4 == 6)) {
			f.LinkType = 101
		}
	}
	if v := attrs[15]; len(v) > 0 {
		f.Fields["psample_user_cookie"] = append([]byte(nil), v...)
	}
	return f, true, nil
}

func (d *decoder) port(id uint64) (int, string, bool) {
	key := strconv.FormatUint(id, 10)
	if value, ok := d.ports[key]; ok {
		if index, err := strconv.Atoi(value); err == nil && index > 0 {
			return index, "", true
		}
		if iface, err := net.InterfaceByName(value); err == nil {
			return iface.Index, iface.Name, true
		}
		return 0, value, false
	}
	// Standard psample uses Linux ifindexes. An explicit map makes unmatched
	// vendor identifiers opaque rather than incorrectly treating them as indexes.
	if len(d.ports) == 0 && id > 0 && id <= uint64(^uint32(0)>>1) {
		return int(id), "", true
	}
	return 0, "", false
}
