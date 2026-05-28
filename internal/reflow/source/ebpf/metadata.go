package ebpf

import (
	"net"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
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
	metadataMapFD         int
	conntrack             *conntrackTracker
}

// InitEvents emits a source_init control event so template-based encoders can
// learn source-scoped metadata before the first captured packet arrives.
func (s *Source) InitEvents() ([]*event.Event, error) {
	sourceID := s.sourceID()
	return []*event.Event{
		{
			ReceivedAt: time.Now().UTC(),
			Kind:       "control",
			Source: event.SourceMetadata{
				Network:               s.cfg.Network,
				Address:               s.cfg.Interface,
				Type:                  s.cfg.Type,
				CaptureInterface:      s.cfg.Interface,
				CaptureInterfaceIndex: s.captureInterfaceIndex,
				AgentIP:               s.agentIP,
				SourceID:              sourceID,
				SourceIDSet:           true,
				Sampling: &event.SamplingMetadata{
					Rate:       uint32(s.cfg.SampleEvery),
					SamplePool: 0,
					Drops:      0,
				},
			},
			Control: &event.ControlMetadata{
				Type:   "source_init",
				Stream: "options_data",
			},
			Fields: map[string]any{
				"input_if":  uint32(s.captureInterfaceIndex),
				"output_if": uint32(s.captureInterfaceIndex),
			},
			Payload: event.SourceInit{
				Stream:       "options_data",
				AgentIP:      s.agentIP,
				SourceID:     sourceID,
				SamplingRate: uint32(s.cfg.SampleEvery),
				SamplePool:   0,
				Drops:        0,
				InputIf:      uint32(s.captureInterfaceIndex),
				OutputIf:     uint32(s.captureInterfaceIndex),
			},
		},
	}, nil
}

func (s *Source) shouldEmitCurrentPacket() bool {
	if s.cfg.SampleEvery <= 1 {
		return true
	}
	index := int((s.seenCount - 1) % uint64(s.cfg.SampleEvery))
	return index == s.cfg.SampleOffset
}

func (s *Source) sourceID() uint32 {
	if s.cfg.SourceID != nil {
		return *s.cfg.SourceID
	}
	return uint32(s.captureInterfaceIndex)
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
