//go:build freebsd && !reflow_pcap && !reflow_dpdk

package bpf

import (
	"fmt"
	"golang.org/x/sys/unix"
	"unsafe"
)

func timestampSize() int { return int(unsafe.Sizeof(unix.Timeval{})) }
func setDirection(fd int, direction string) error {
	v := uint32(1)
	switch direction {
	case "ingress":
		v = 0
	case "egress":
		v = 2
	case "", "both":
		return nil
	default:
		return fmt.Errorf("unsupported BPF direction %q", direction)
	}
	if err := ioctl(fd, unix.BIOCSDIRECTION, unsafe.Pointer(&v)); err != nil {
		return fmt.Errorf("BPF direction: %w", err)
	}
	return nil
}
