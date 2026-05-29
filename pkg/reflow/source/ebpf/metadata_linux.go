//go:build linux && !reflow_noebpf

package ebpf

import (
	"net"
	"sync"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
)

type Source struct {
	cfg                   config.SourceConfig
	agentIP               string
	captureInterfaceIndex int
	interfaceNames        map[uint32]string
	seenCount             uint64
	mu                    sync.Mutex
	fd                    int
	progFD                int
	eventMapFD            int
	perfEvents            []*perfEventReader
	conntrack             *conntrackTracker
}

func (s *Source) shouldEmitCurrentPacket() bool {
	if s.cfg.SampleEvery <= 1 {
		return true
	}
	index := int((s.seenCount - 1) % uint64(s.cfg.SampleEvery))
	return index == s.cfg.SampleOffset
}

// firstInterfaceIP picks a stable agent IP for exported metadata, preferring
// non-loopback IPv4, then any non-loopback address, then localhost.
func firstInterfaceIP(iface *net.Interface) string {
	addrs, err := iface.Addrs()
	if err != nil {
		return "127.0.0.1"
	}
	var fallback string
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String()
		}
		if fallback == "" {
			fallback = ip.String()
		}
	}
	if fallback != "" {
		return fallback
	}
	return "127.0.0.1"
}
