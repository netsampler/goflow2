// Package capture adapts capture engines to Reflow's source lifecycle. Driver
// buffers are borrowed only for the duration of the synchronous emit callback.
package capture

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/event"
)

type Frame struct {
	Data           []byte
	Timestamp      time.Time
	LinkType       uint32
	Interface      string
	InterfaceIndex int
	Direction      string
	PacketType     string
	CaptureLength  int
	WireLength     int // Zero means the engine did not report the original length.
	Fields         map[string]any
	Internal       map[string]any
	Sampling       *event.SamplingMetadata
	Drops          uint64 // Capture delivery loss, never the number of sampled-out packets.
}

type Driver interface {
	Run(context.Context, func(Frame) error) error
	Close() error
}

type Source struct {
	cfg              config.SourceConfig
	driver           Driver
	filter           *regexp.Regexp
	mu               sync.Mutex
	initialized      map[int]bool
	agentIPs         map[int]string
	interfaces       map[int]string
	interfaceIndexes map[string]int
	seen             uint64
	drops            uint64
	reportedDrops    uint64
	lastSampling     *event.SamplingMetadata
	closeOnce        sync.Once
	closeErr         error
}

func New(cfg config.SourceConfig, driver Driver) (*Source, error) {
	if err := config.NormalizeCapture(&cfg); err != nil {
		return nil, err
	}
	var filter *regexp.Regexp
	if cfg.Capture.InterfaceFilter != "" {
		filter = regexp.MustCompile(cfg.Capture.InterfaceFilter)
	}
	return &Source{cfg: cfg, driver: driver, filter: filter, initialized: make(map[int]bool), agentIPs: make(map[int]string), interfaces: make(map[int]string), interfaceIndexes: make(map[string]int)}, nil
}

func (s *Source) InitEvents() ([]*event.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.drops > s.reportedDrops {
		slog.Warn("capture packets dropped", "network", s.cfg.Network,
			"interface", s.cfg.Capture.Interface, "dropped", s.drops-s.reportedDrops,
			"total_dropped", s.drops)
		s.reportedDrops = s.drops
	}
	name := s.cfg.Capture.Interface
	if name == "" {
		return []*event.Event{s.initEvent(Frame{})}, nil
	}
	var ifaces []net.Interface
	if name == "any" || name == "all" {
		var err error
		ifaces, err = net.Interfaces()
		if err != nil {
			return nil, err
		}
	} else {
		iface, err := net.InterfaceByName(name)
		// DPDK ports, Windows friendly names and libpcap device
		// identifiers are not necessarily operating-system netdev names.
		if err != nil {
			return []*event.Event{s.initEvent(Frame{})}, nil
		}
		ifaces = []net.Interface{*iface}
	}
	sort.Slice(ifaces, func(i, j int) bool { return ifaces[i].Index < ifaces[j].Index })
	out := make([]*event.Event, 0, len(ifaces))
	for _, iface := range ifaces {
		if s.filter != nil && !s.filter.MatchString(iface.Name) {
			continue
		}
		s.initialized[iface.Index] = true
		s.interfaces[iface.Index] = iface.Name
		s.interfaceIndexes[iface.Name] = iface.Index
		out = append(out, s.initEvent(Frame{Interface: iface.Name, InterfaceIndex: iface.Index}))
	}
	return out, nil
}

func (s *Source) metadata(f Frame) event.SourceMetadata {
	sourceID := uint32(0)
	if s.cfg.SourceID != nil {
		sourceID = *s.cfg.SourceID
	} else if f.InterfaceIndex > 0 {
		sourceID = uint32(f.InterfaceIndex)
	}
	agentIP, ok := s.agentIPs[f.InterfaceIndex]
	if !ok {
		agentIP = interfaceIP(f.InterfaceIndex)
		s.agentIPs[f.InterfaceIndex] = agentIP
	}
	rate := uint32(s.cfg.Capture.SampleEvery)
	sampling := &event.SamplingMetadata{Rate: rate, SamplePool: uint32(s.seen), CaptureDropsTotal: f.Drops}
	if s.cfg.Capture.Sampling.SelectsInDriver() {
		// The backend only delivers selected packets; the eligible packet pool is
		// unknown rather than the number of samples received.
		sampling.SamplePool = 0
	}
	if f.Sampling != nil {
		// Preserve exporter counters rather than synthesizing a sample pool from
		// received samples; combine known sampling divisors for downstream exporters.
		copySampling := *f.Sampling
		sampling = &copySampling
		sampling.CaptureDropsTotal = f.Drops
		if s.cfg.Capture.Sampling.SelectsInDriver() {
			rate = 1           // The backend already applied this local divisor.
			sampling.Drops = 0 // Capture loss has its own 64-bit total counter.
		}
		effective := uint64(copySampling.Rate) * uint64(rate)
		if effective <= uint64(^uint32(0)) {
			sampling.Rate = uint32(effective)
		} else {
			sampling.Rate = 0
		}
	}
	return event.SourceMetadata{Network: s.cfg.Network, Address: s.cfg.Address, Type: s.cfg.Type, CaptureInterface: f.Interface, CaptureInterfaceIndex: f.InterfaceIndex, CaptureDirection: f.Direction, CapturePacketType: f.PacketType, AgentIP: agentIP, SourceID: sourceID, SourceIDSet: true, Sampling: sampling, JSON: event.JSONMetadata{Flavor: s.cfg.JSON.Flavor}}
}
func interfaceIP(index int) string {
	iface, err := net.InterfaceByIndex(index)
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	var fallback string
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			continue
		}
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String()
		}
		fallback = ip.String()
	}
	return fallback
}
func (s *Source) initEvent(f Frame) *event.Event {
	// Refreshes have no packet frame. Preserve the backend's cumulative loss
	// instead of advertising a fresh zero, including between sampled packets.
	f.Drops = s.drops
	f.Sampling = s.lastSampling
	meta := s.metadata(f)
	fields := map[string]any{}
	if f.InterfaceIndex > 0 {
		fields["input_if"] = uint32(f.InterfaceIndex)
		fields["output_if"] = uint32(f.InterfaceIndex)
	}
	return &event.Event{ReceivedAt: time.Now().UTC(), Kind: "control", Source: meta, Control: &event.ControlMetadata{Type: "source_init", Stream: "options_data"}, Payload: event.SourceInit{Stream: "options_data", AgentIP: meta.AgentIP, SourceID: meta.SourceID, SamplingRate: meta.Sampling.Rate, SamplePool: meta.Sampling.SamplePool, Drops: meta.Sampling.Drops, CaptureDropsTotal: meta.Sampling.CaptureDropsTotal, InputIf: uint32(f.InterfaceIndex), OutputIf: uint32(f.InterfaceIndex)}, Fields: fields}
}

func (s *Source) Start(ctx context.Context, emit func(*event.Event) error) error {
	defer func() { _ = s.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	return s.driver.Run(ctx, func(f Frame) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cfg.Capture.Sampling.SelectsInDriver() && f.Sampling != nil {
			copySampling := *f.Sampling
			s.lastSampling = &copySampling
		}
		if f.Drops > s.drops {
			s.drops = f.Drops
		}
		fieldsCopy := make(map[string]any, len(f.Fields)+6)
		for k, v := range f.Fields {
			fieldsCopy[k] = v
		}
		f.Fields = fieldsCopy
		if f.CaptureLength == 0 {
			f.CaptureLength = len(f.Data)
		}
		if len(f.Data) > s.cfg.Capture.SnapLen {
			f.Data = f.Data[:s.cfg.Capture.SnapLen]
			f.CaptureLength = len(f.Data)
		}
		f = NormalizeFrame(f)
		if f.InterfaceIndex == 0 && f.Interface != "" {
			if index, ok := s.interfaceIndexes[f.Interface]; ok {
				f.InterfaceIndex = index
			} else {
				index := 0
				if iface, err := net.InterfaceByName(f.Interface); err == nil {
					index = iface.Index
				}
				s.interfaceIndexes[f.Interface] = index
				f.InterfaceIndex = index
			}
		}
		if f.Interface == "" && f.InterfaceIndex > 0 {
			if name, ok := s.interfaces[f.InterfaceIndex]; ok {
				f.Interface = name
			} else if iface, err := net.InterfaceByIndex(f.InterfaceIndex); err == nil {
				f.Interface = iface.Name
				s.interfaces[iface.Index] = iface.Name
				s.interfaceIndexes[iface.Name] = iface.Index
			}
		}
		if f.Direction != "" {
			f.Fields["capture_direction"] = f.Direction
		}
		if f.PacketType != "" {
			f.Fields["capture_packet_type"] = f.PacketType
		}
		if f.InterfaceIndex > 0 {
			key := ""
			switch f.Direction {
			case "ingress":
				key = "input_if"
			case "egress":
				key = "output_if"
			}
			if key != "" {
				if _, exists := f.Fields[key]; !exists {
					f.Fields[key] = uint32(f.InterfaceIndex)
				}
			}
		}
		if f.Interface != "" {
			for _, key := range []string{"input_if", "output_if"} {
				if idx, ok := f.Fields[key].(uint32); ok && int(idx) == f.InterfaceIndex {
					if key == "input_if" {
						f.Fields["input_interface"] = f.Interface
					} else {
						f.Fields["output_interface"] = f.Interface
					}
				}
			}
		}
		if s.filter != nil && !s.filter.MatchString(f.Interface) {
			return nil
		}
		if s.cfg.Capture.Direction != "both" && f.Direction != s.cfg.Capture.Direction {
			return nil
		}
		s.seen++
		if f.InterfaceIndex > 0 && !s.initialized[f.InterfaceIndex] {
			s.initialized[f.InterfaceIndex] = true
			if err := emit(s.initEvent(f)); err != nil {
				return err
			}
		}
		if !s.cfg.Capture.Sampling.SelectsInDriver() && (s.seen-1)%uint64(s.cfg.Capture.SampleEvery) != uint64(s.cfg.Capture.SampleOffset) {
			return nil
		}
		if f.CaptureLength == 0 {
			f.CaptureLength = len(f.Data)
		}
		fields := f.Fields
		fields["capture_length"] = f.CaptureLength
		if f.WireLength > 0 {
			fields["wire_length"] = f.WireLength
		}
		if f.Drops > 0 {
			fields["capture_drops"] = f.Drops
		}
		if f.Sampling != nil {
			if s.cfg.Capture.Sampling.Location == "callback" {
				fields["sampling_location"] = "callback"
				fields["callback_sample_every"] = f.Sampling.Rate
			} else {
				fields["upstream_sampling_rate"] = f.Sampling.Rate
			}
			localRate := s.cfg.Capture.SampleEvery
			if s.cfg.Capture.Sampling.SelectsInDriver() {
				localRate = 1
			}
			fields["effective_sampling_rate"] = uint64(f.Sampling.Rate) * uint64(localRate)
			fields["local_sample_every"] = localRate
		}
		timestamp := f.Timestamp.UTC()
		if timestamp.IsZero() {
			timestamp = time.Now().UTC()
		}
		evt := &event.Event{ReceivedAt: timestamp, Source: s.metadata(f), Payload: append([]byte(nil), f.Data...), Fields: fields, Internal: f.Internal}
		return emit(evt)
	})
}
func (s *Source) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.driver.Close() })
	return s.closeErr
}

// Unsupported supplies a consistent actionable error without opening resources.
func Unsupported(network, reason string) error {
	return fmt.Errorf("capture input %s unavailable: %s", network, reason)
}
