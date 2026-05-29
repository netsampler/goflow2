package ebpf

import (
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

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

func (s *Source) sourceID() uint32 {
	if s.cfg.SourceID != nil {
		return *s.cfg.SourceID
	}
	return uint32(s.captureInterfaceIndex)
}
