//go:build darwin && !reflow_pcap && !reflow_dpdk

package bpf

import (
	"fmt"
	"unsafe"
)

func timestampSize() int { return 8 }
func setDirection(fd int, direction string) error {
	// BIOCSDIRECTION is a Darwin private ioctl. A failed capability request is
	// surfaced, rather than silently capturing the opposite direction.
	v := uint32(3)
	switch direction {
	case "ingress":
		v = 1
	case "egress":
		v = 2
	case "", "both":
		return nil
	default:
		return fmt.Errorf("unsupported BPF direction %q", direction)
	}
	if err := ioctl(fd, 0x8004428b, unsafe.Pointer(&v)); err != nil {
		return fmt.Errorf("BPF direction: %w", err)
	}
	return nil
}
