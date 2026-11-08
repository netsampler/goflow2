//go:build netbsd && !reflow_pcap && !reflow_dpdk && !reflow_pfring

package bpf

import (
	"fmt"
	"golang.org/x/sys/unix"
	"unsafe"
)

func timestampSize() int { return int(unsafe.Sizeof(unix.BpfTimeval{})) }
func setDirection(fd int, direction string) error {
	if direction == "" || direction == "both" {
		return nil
	}
	if direction != "ingress" {
		return fmt.Errorf("NetBSD BPF supports ingress or both directions")
	}
	v := uint32(0)
	if err := ioctl(fd, unix.BIOCSSEESENT, unsafe.Pointer(&v)); err != nil {
		return fmt.Errorf("BPF direction: %w", err)
	}
	return nil
}
