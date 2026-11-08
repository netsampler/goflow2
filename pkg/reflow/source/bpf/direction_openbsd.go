//go:build openbsd && !reflow_pcap && !reflow_dpdk

package bpf

import (
	"fmt"
	"golang.org/x/sys/unix"
	"unsafe"
)

func timestampSize() int { return 8 }
func setDirection(fd int, direction string) error {
	v := uint32(0)
	switch direction {
	case "ingress":
		v = unix.BPF_DIRECTION_OUT
	case "egress":
		v = unix.BPF_DIRECTION_IN
	case "", "both":
		return nil
	default:
		return fmt.Errorf("unsupported BPF direction %q", direction)
	}
	if err := ioctl(fd, unix.BIOCSDIRFILT, unsafe.Pointer(&v)); err != nil {
		return fmt.Errorf("BPF direction: %w", err)
	}
	return nil
}
