//go:build linux && !reflow_pcap && !reflow_dpdk

package ebpf

import (
	"context"
	"fmt"
	"sync"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func New(cfg config.SourceConfig) (*Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	if cfg.Capture.Promiscuous != nil && *cfg.Capture.Promiscuous {
		return nil, fmt.Errorf("ebpf capture does not support promiscuous mode")
	}
	s, err := newSource(cfg)
	if err != nil {
		return nil, err
	}
	adapter, err := capture.New(cfg, &captureDriver{source: s})
	if err != nil {
		return nil, err
	}
	s.shared = adapter
	// Sampling and source initialization are owned by the shared adapter.
	s.cfg.SampleEvery = 1
	s.cfg.SampleOffset = 0
	return s, nil
}
func (s *Source) Start(ctx context.Context, emit func(*event.Event) error) error {
	return s.shared.Start(ctx, emit)
}

// CaptureDrops returns the latest cumulative loss reported to the capture adapter.
func (s *Source) CaptureDrops() uint64 { return s.shared.CaptureDrops() }

func (s *Source) Close() error {
	if s.shared != nil {
		return s.shared.Close()
	}
	return s.closeBackend()
}

type captureDriver struct {
	source *Source
	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

func (d *captureDriver) Run(ctx context.Context, emit func(capture.Frame) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.cancel = cancel
	d.mu.Unlock()
	return d.source.run(ctx, func(evt *event.Event) error {
		if evt.Kind == "control" {
			return nil
		}
		data, ok := evt.Payload.([]byte)
		if !ok {
			return fmt.Errorf("ebpf capture returned non-packet payload")
		}
		direction := evt.Source.CaptureDirection
		switch direction {
		case "in":
			direction = "ingress"
		case "out":
			direction = "egress"
		}
		wireLength, _ := evt.Fields["wire_length"].(int)
		var sampling *event.SamplingMetadata
		if d.source.cfg.Capture.Sampling.Location == "kernel" {
			sampling = evt.Source.Sampling
		}
		return emit(capture.Frame{Data: data, Timestamp: evt.ReceivedAt, LinkType: 1, Interface: evt.Source.CaptureInterface, InterfaceIndex: evt.Source.CaptureInterfaceIndex, Direction: direction, PacketType: evt.Source.CapturePacketType, CaptureLength: len(data), WireLength: wireLength, Fields: evt.Fields, Internal: evt.Internal, Sampling: sampling, Drops: d.source.captureDropCount()})
	})
}
func (d *captureDriver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.cancel != nil {
		d.cancel()
	}
	return nil
}
