//go:build !reflow_nopcap

package pcap

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/gopacket/pcap"

	"github.com/netsampler/goflow2/v3/internal/reflow/config"
	"github.com/netsampler/goflow2/v3/internal/reflow/event"
)

type Source struct {
	cfg                   config.SourceConfig
	agentIP               string
	handle                *pcap.Handle
	captureInterfaceIndex int
	wg                    sync.WaitGroup
	seenCount             uint64
}

// New validates a live-capture source and precomputes interface metadata used
// in emitted events and source_init control records.
func New(cfg config.SourceConfig) (*Source, error) {
	if cfg.Network != "pcap_live" {
		return nil, fmt.Errorf("unsupported source.network %q", cfg.Network)
	}
	if cfg.Interface == "" {
		return nil, fmt.Errorf("source.interface is required when source.network=pcap_live")
	}
	if cfg.Type == "" {
		cfg.Type = "bytes"
	}
	if cfg.SnapLen <= 0 {
		cfg.SnapLen = 65535
	}
	if cfg.SampleEvery <= 0 {
		cfg.SampleEvery = 1
	}
	iface, err := net.InterfaceByName(cfg.Interface)
	if err != nil {
		return nil, fmt.Errorf("lookup capture interface %s: %w", cfg.Interface, err)
	}
	agentIP := firstInterfaceIP(iface)
	return &Source{
		cfg:                   cfg,
		agentIP:               agentIP,
		captureInterfaceIndex: iface.Index,
	}, nil
}

// InitEvents emits a source_init control event so template-based encoders can
// learn source-scoped metadata before the first captured packet arrives.
func (s *Source) InitEvents() ([]*event.Event, error) {
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
			},
			Control: &event.ControlMetadata{
				Type:   "source_init",
				Stream: "options_data",
			},
			Fields: map[string]any{
				"agent_ip":      s.agentIP,
				"source_id":     uint32(s.captureInterfaceIndex),
				"sampling_rate": uint32(s.cfg.SampleEvery),
				"sample_pool":   uint32(0),
				"drops":         uint32(0),
				"input_if":      uint32(s.captureInterfaceIndex),
				"output_if":     uint32(s.captureInterfaceIndex),
			},
			Payload: event.SourceInit{
				Stream:       "options_data",
				AgentIP:      s.agentIP,
				SourceID:     uint32(s.captureInterfaceIndex),
				SamplingRate: uint32(s.cfg.SampleEvery),
				SamplePool:   0,
				Drops:        0,
				InputIf:      uint32(s.captureInterfaceIndex),
				OutputIf:     uint32(s.captureInterfaceIndex),
			},
		},
	}, nil
}

// Start reads packets from libpcap, applies packet-level sampling, and emits
// one raw bytes event per accepted capture.
func (s *Source) Start(ctx context.Context, emit func(*event.Event) error) error {
	handle, err := pcap.OpenLive(s.cfg.Interface, int32(s.cfg.SnapLen), true, 500*time.Millisecond)
	if err != nil {
		return fmt.Errorf("open pcap device %s: %w", s.cfg.Interface, err)
	}
	s.handle = handle

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		<-ctx.Done()
		if s.handle != nil {
			s.handle.Close()
		}
	}()

	for {
		data, ci, err := s.handle.ReadPacketData()
		if err != nil {
			switch err {
			case pcap.NextErrorTimeoutExpired:
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			case pcap.NextErrorNotActivated:
				return fmt.Errorf("read pcap packet: %w", err)
			default:
				select {
				case <-ctx.Done():
					return nil
				default:
					return fmt.Errorf("read pcap packet: %w", err)
				}
			}
		}
		s.seenCount++
		if !s.shouldEmitCurrentPacket() {
			continue
		}
		// Drop counters come from the capture engine, not from packet contents.
		drops := s.currentDropCount()

		evt := &event.Event{
			ReceivedAt: time.Now().UTC(),
			Source: event.SourceMetadata{
				Network:               s.cfg.Network,
				Address:               s.cfg.Interface,
				Type:                  s.cfg.Type,
				CaptureInterface:      s.cfg.Interface,
				CaptureInterfaceIndex: s.captureInterfaceIndex,
				JSON: event.JSONMetadata{
					Flavor: s.cfg.JSON.Flavor,
				},
			},
			SFlow: &event.SFlowMetadata{
				AgentIP:      s.agentIP,
				SamplingRate: uint32(s.cfg.SampleEvery),
				SamplePool:   uint32(s.seenCount),
				Drops:        drops,
			},
			Payload: append([]byte(nil), data...),
			Fields: map[string]any{
				"agent_ip":       s.agentIP,
				"sampling_rate":  uint32(s.cfg.SampleEvery),
				"sample_pool":    uint32(s.seenCount),
				"drops":          drops,
				"capture_length": ci.CaptureLength,
				"wire_length":    ci.Length,
			},
		}

		if err := emit(evt); err != nil {
			return err
		}
	}
}

// currentDropCount asks libpcap for the current cumulative drop counters and
// folds both capture and interface drops into one exported value.
func (s *Source) currentDropCount() uint32 {
	if s.handle == nil {
		return 0
	}
	stats, err := s.handle.Stats()
	if err != nil || stats == nil {
		return 0
	}
	return uint32(stats.PacketsDropped + stats.PacketsIfDropped)
}

// shouldEmitCurrentPacket applies deterministic packet sampling based on
// sample_every/sample_offset so replay and testing stay reproducible.
func (s *Source) shouldEmitCurrentPacket() bool {
	if s.cfg.SampleEvery <= 1 {
		return true
	}
	index := int((s.seenCount - 1) % uint64(s.cfg.SampleEvery))
	return index == s.cfg.SampleOffset
}

// Close terminates the capture handle and waits for the shutdown goroutine.
func (s *Source) Close() error {
	if s.handle != nil {
		s.handle.Close()
	}
	s.wg.Wait()
	return nil
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
