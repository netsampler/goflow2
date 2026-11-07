// Package buildprofile describes compiled capture support without importing any
// capture driver or C SDK, so configuration and help remain lightweight.
package buildprofile

import (
	"fmt"
	"runtime"
	"slices"
)

func Inputs() []string {
	switch Name {
	case "pcap":
		return []string{"pcap_live"}
	case "dpdk":
		if runtime.GOOS == "linux" {
			return []string{"dpdk"}
		}
	case "pfring":
		if runtime.GOOS == "linux" {
			if PFRingZC {
				return []string{"pfring", "pfring_zc"}
			}
			return []string{"pfring"}
		}
	default:
		switch runtime.GOOS {
		case "linux":
			return []string{"af_packet", "ebpf", "xdp", "nflog", "psample"}
		case "darwin":
			return []string{"bpf", "pktap"}
		case "freebsd", "openbsd", "netbsd":
			return []string{"bpf"}
		case "windows":
			return []string{"pktmon"}
		}
	}
	return nil
}
func Supports(input string) bool { return slices.Contains(Inputs(), input) }
func Summary() string {
	return fmt.Sprintf("profile=%s target=%s/%s capture=%v", Name, runtime.GOOS, runtime.GOARCH, Inputs())
}
