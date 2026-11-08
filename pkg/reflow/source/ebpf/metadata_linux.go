//go:build linux && !reflow_pcap && !reflow_dpdk

package ebpf

import (
	"net"
	"regexp"
	"sort"
	"sync"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

type Source struct {
	shared                *capture.Source
	cfg                   config.SourceConfig
	agentIP               string
	captureInterfaceIndex int
	captureAny            bool
	interfaceFilter       *regexp.Regexp
	interfaceNames        map[uint32]string
	initializedInterfaces map[uint32]struct{}
	seenCount             uint64
	mu                    sync.Mutex
	fd                    int
	progFD                int
	eventMapFD            int
	perfEvents            []*perfEventReader
	conntrack             *conntrackTracker
	kernelSampler         *capture.PerfSampler
	backendSnaplen        int
}

func (s *Source) shouldEmitCurrentPacket() bool {
	if s.cfg.SampleEvery <= 1 {
		return true
	}
	index := int((s.seenCount - 1) % uint64(s.cfg.SampleEvery))
	return index == s.cfg.SampleOffset
}

func (s *Source) sourceID() uint32 {
	return s.sourceIDForInterface(0)
}

func (s *Source) interfaceInitialized(ifIndex uint32) bool {
	if ifIndex == 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.initializedInterfaces[ifIndex]
	return ok
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

func firstSystemIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1"
	}
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].Index < ifaces[j].Index
	})
	for i := range ifaces {
		if ip := firstInterfaceIP(&ifaces[i]); ip != "127.0.0.1" {
			return ip
		}
	}
	return "127.0.0.1"
}

// Refresh from the independent monitor even if no frame has reached the shared
// adapter since a rate change or output failure.
func (s *Source) refreshSampling(events []*event.Event) {
	s.mu.Lock()
	sampler := s.kernelSampler
	s.mu.Unlock()
	if sampler == nil {
		return
	}
	for _, evt := range events {
		evt.Source.Sampling.CaptureDropsTotal = max(evt.Source.Sampling.CaptureDropsTotal, sampler.Drops())
		if s.cfg.Capture.Sampling.Location == "kernel" {
			evt.Source.Sampling.Rate = sampler.Rate()
		}
		if payload, ok := evt.Payload.(event.SourceInit); ok {
			payload.CaptureDropsTotal = evt.Source.Sampling.CaptureDropsTotal
			payload.SamplingRate = evt.Source.Sampling.Rate
			evt.Payload = payload
		}
	}
}
