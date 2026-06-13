package ebpf

import (
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

// InitEvents emits a source_init control event so template-based encoders can
// learn source-scoped metadata before the first captured packet arrives.
func (s *Source) InitEvents() ([]*event.Event, error) {
	if !s.captureAny {
		iface := net.Interface{
			Index: s.captureInterfaceIndex,
			Name:  s.cfg.Interface,
		}
		s.markInterfaceInitialized(uint32(iface.Index), iface.Name)
		return []*event.Event{s.sourceInitEvent(iface, s.agentIP, 0)}, nil
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list capture interfaces: %w", err)
	}
	sort.Slice(ifaces, func(i, j int) bool {
		return ifaces[i].Index < ifaces[j].Index
	})
	events := make([]*event.Event, 0, len(ifaces))
	for _, iface := range ifaces {
		if !s.interfaceAllowed(iface.Name) {
			continue
		}
		agentIP := firstInterfaceIP(&iface)
		s.markInterfaceInitialized(uint32(iface.Index), iface.Name)
		events = append(events, s.sourceInitEvent(iface, agentIP, 0))
	}
	return events, nil
}

func (s *Source) sourceInitEvent(iface net.Interface, agentIP string, drops uint32) *event.Event {
	ifIndex := uint32(iface.Index)
	sourceID := s.sourceIDForInterface(ifIndex)
	return &event.Event{
		ReceivedAt: time.Now().UTC(),
		Kind:       "control",
		Source: event.SourceMetadata{
			Network:               s.cfg.Network,
			Address:               s.cfg.Interface,
			Type:                  s.cfg.Type,
			CaptureInterface:      iface.Name,
			CaptureInterfaceIndex: iface.Index,
			AgentIP:               agentIP,
			SourceID:              sourceID,
			SourceIDSet:           true,
			Sampling: &event.SamplingMetadata{
				Rate:       uint32(s.cfg.SampleEvery),
				SamplePool: 0,
				Drops:      drops,
			},
		},
		Control: &event.ControlMetadata{
			Type:   "source_init",
			Stream: "options_data",
		},
		Fields: map[string]any{
			"input_if":  ifIndex,
			"output_if": ifIndex,
		},
		Payload: event.SourceInit{
			Stream:       "options_data",
			AgentIP:      agentIP,
			SourceID:     sourceID,
			SamplingRate: uint32(s.cfg.SampleEvery),
			SamplePool:   0,
			Drops:        drops,
			InputIf:      ifIndex,
			OutputIf:     ifIndex,
		},
	}
}

func (s *Source) sourceID() uint32 {
	return s.sourceIDForInterface(0)
}

func (s *Source) sourceIDForInterface(ifIndex uint32) uint32 {
	if s.cfg.SourceID != nil {
		return *s.cfg.SourceID
	}
	if s.captureAny && ifIndex != 0 {
		return ifIndex
	}
	return uint32(s.captureInterfaceIndex)
}

func (s *Source) markInterfaceInitialized(ifIndex uint32, name string) {
	if ifIndex == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.interfaceNames == nil {
		s.interfaceNames = make(map[uint32]string)
	}
	if name != "" {
		s.interfaceNames[ifIndex] = name
	}
	if s.initializedInterfaces == nil {
		s.initializedInterfaces = make(map[uint32]struct{})
	}
	s.initializedInterfaces[ifIndex] = struct{}{}
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

func (s *Source) interfaceAllowed(name string) bool {
	if s.interfaceFilter == nil {
		return true
	}
	return name != "" && s.interfaceFilter.MatchString(name)
}
