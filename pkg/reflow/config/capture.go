package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// CaptureConfig contains options common to live packet inputs. Backend-specific
// settings are deliberately separate from event identity and pipeline stages.
type CaptureConfig struct {
	Interface       string         `yaml:"interface,omitempty"`
	SnapLen         int            `yaml:"snaplen,omitempty"`
	SampleEvery     int            `yaml:"sample_every,omitempty"`
	SampleOffset    int            `yaml:"sample_offset,omitempty"`
	InterfaceFilter string         `yaml:"interface_filter,omitempty"`
	Direction       string         `yaml:"direction,omitempty"`
	Promiscuous     *bool          `yaml:"promiscuous,omitempty"`
	EBPF            EBPFConfig     `yaml:"ebpf,omitempty"`
	AFPacket        AFPacketConfig `yaml:"af_packet,omitempty"`
	XDP             XDPConfig      `yaml:"xdp,omitempty"`
	NFLog           NFLogConfig    `yaml:"nflog,omitempty"`
	PSample         PSampleConfig  `yaml:"psample,omitempty"`
	Pktap           PktapConfig    `yaml:"pktap,omitempty"`
	Pktmon          PktmonConfig   `yaml:"pktmon,omitempty"`
	DPDK            DPDKConfig     `yaml:"dpdk,omitempty"`
	PFRing          PFRingConfig   `yaml:"pfring,omitempty"`
}

type AFPacketConfig struct {
	BlockSize      int `yaml:"block_size,omitempty"`
	NumBlocks      int `yaml:"num_blocks,omitempty"`
	BlockTimeoutMS int `yaml:"block_timeout_ms,omitempty"`
}
type XDPConfig struct {
	Mode string `yaml:"mode,omitempty"`
}
type NFLogConfig struct {
	Group      uint16 `yaml:"group"`
	BufferSize int    `yaml:"buffer_size,omitempty"`
}
type PSampleConfig struct {
	Groups     []uint32          `yaml:"groups,omitempty"`
	PortMap    map[string]string `yaml:"port_map,omitempty"`
	BufferSize int               `yaml:"buffer_size,omitempty"`
}
type PktapConfig struct {
	Interfaces []string `yaml:"interfaces,omitempty"`
}
type PktmonConfig struct {
	QueueSize int `yaml:"queue_size,omitempty"`
}
type DPDKConfig struct {
	Port        string   `yaml:"port,omitempty"`
	Queues      []uint16 `yaml:"queues,omitempty"`
	EALArgs     []string `yaml:"eal_args,omitempty"`
	Descriptors int      `yaml:"descriptors,omitempty"`
	Mbufs       int      `yaml:"mbufs,omitempty"`
}
type PFRingConfig struct {
	ClusterID   int    `yaml:"cluster_id,omitempty"`
	ClusterType string `yaml:"cluster_type,omitempty"`
	ZCClusterID uint32 `yaml:"zc_cluster_id,omitempty"`
	ZCPoolID    uint32 `yaml:"zc_pool_id,omitempty"`
	ZCQueueID   uint32 `yaml:"zc_queue_id,omitempty"`
	ZCIPC       bool   `yaml:"zc_ipc,omitempty"`
	Buffers     uint32 `yaml:"buffers,omitempty"`
}

func IsCaptureNetwork(network string) bool {
	switch network {
	case "af_packet", "ebpf", "xdp", "nflog", "psample", "bpf", "pktap", "pktmon", "pcap_live", "dpdk", "pfring", "pfring_zc":
		return true
	}
	return false
}

// NormalizeCapture also populates deprecated Go fields used by existing callers.
// YAML output uses only the capture block; legacy flat YAML remains readable.
func NormalizeCapture(s *SourceConfig) error {
	c := &s.Capture
	if c.Interface == "" {
		c.Interface = s.Interface
	}
	if c.SnapLen == 0 {
		c.SnapLen = s.SnapLen
	}
	if c.SampleEvery == 0 {
		c.SampleEvery = s.SampleEvery
		if c.SampleOffset == 0 {
			c.SampleOffset = s.SampleOffset
		}
	}
	if c.InterfaceFilter == "" {
		c.InterfaceFilter = s.InterfaceFilter
	}
	if c.EBPF.IsZero() {
		c.EBPF = s.EBPF
	}
	if c.Direction == "" {
		c.Direction = c.EBPF.Direction
	}
	if c.Direction == "" {
		switch s.Network {
		case "xdp", "dpdk", "pfring_zc":
			c.Direction = "ingress"
		default:
			c.Direction = "both"
		}
	}
	var err error
	c.Direction, err = NormalizeEBPFDirection(c.Direction)
	if err != nil {
		return fmt.Errorf("source.capture.direction: %w", err)
	}
	if s.Network == "ebpf" {
		c.EBPF.Direction = c.Direction
	}
	if c.SnapLen <= 0 {
		c.SnapLen = 65535
		if s.Network == "pktmon" {
			c.SnapLen = 9000
		}
	}
	if s.Network == "pktmon" && c.SnapLen > 9000 {
		return fmt.Errorf("source.capture.snaplen exceeds pktmon maximum 9000")
	}
	if c.SampleEvery <= 0 {
		c.SampleEvery = 1
	}
	if uint64(c.SampleEvery) > uint64(^uint32(0)) {
		return fmt.Errorf("source.capture.sample_every exceeds uint32")
	}
	if c.SampleOffset < 0 || c.SampleOffset >= c.SampleEvery {
		return fmt.Errorf("source.sample_offset must be >= 0 and < source.sample_every")
	}
	if c.InterfaceFilter != "" {
		if _, err := regexp.Compile(c.InterfaceFilter); err != nil {
			return fmt.Errorf("source.interface_filter is invalid: %w", err)
		}
	}
	switch s.Network {
	case "nflog", "psample":
	case "dpdk":
		if c.DPDK.Port == "" {
			c.DPDK.Port = c.Interface
		}
		if c.DPDK.Port == "" {
			return fmt.Errorf("source.capture.dpdk.port is required")
		}
	default:
		if c.Interface == "" && (s.Network != "pfring_zc" || !c.PFRing.ZCIPC) {
			return fmt.Errorf("source.capture.interface is required when source.network=%s", s.Network)
		}
	}
	if s.Type == "" {
		s.Type = "bytes"
	}
	if s.Type != "bytes" {
		return fmt.Errorf("source.type must be bytes when source.network=%s", s.Network)
	}
	if s.Address == "" {
		s.Address = c.Interface
	}
	s.Interface, s.SnapLen, s.SampleEvery, s.SampleOffset = c.Interface, c.SnapLen, c.SampleEvery, c.SampleOffset
	s.InterfaceFilter, s.EBPF = c.InterfaceFilter, c.EBPF
	return nil
}

func setCaptureTarget(s *SourceConfig, target string) error {
	switch s.Network {
	case "nflog":
		group, err := strconv.ParseUint(target, 10, 16)
		if err != nil {
			return fmt.Errorf("nflog target must be a group number: %w", err)
		}
		s.Capture.NFLog.Group = uint16(group)
	case "psample":
		if target != "all" {
			for _, part := range strings.Split(target, ",") {
				g, err := strconv.ParseUint(part, 10, 32)
				if err != nil {
					return fmt.Errorf("psample target must be groups or all: %w", err)
				}
				s.Capture.PSample.Groups = append(s.Capture.PSample.Groups, uint32(g))
			}
		}
	case "dpdk":
		s.Capture.DPDK.Port = target
	case "pfring_zc":
		if strings.HasPrefix(target, "ipc:") {
			parts := strings.Split(target, ":")
			if len(parts) != 3 {
				return fmt.Errorf("ZC IPC target must be ipc:cluster:queue")
			}
			cluster, err := strconv.ParseUint(parts[1], 10, 32)
			if err != nil {
				return err
			}
			queue, err := strconv.ParseUint(parts[2], 10, 32)
			if err != nil {
				return err
			}
			s.Capture.PFRing.ZCIPC = true
			s.Capture.PFRing.ZCClusterID = uint32(cluster)
			s.Capture.PFRing.ZCQueueID = uint32(queue)
		} else {
			s.Capture.Interface = target
		}
	default:
		s.Capture.Interface = target
	}
	s.Interface = s.Capture.Interface
	s.Address = ""
	return nil
}

// MarshalYAML keeps generated configurations canonical while allowing old Go
// callers to construct SourceConfig using its original fields.
func (s SourceConfig) MarshalYAML() (any, error) {
	type plain SourceConfig
	if IsCaptureNetwork(s.Network) {
		if err := NormalizeCapture(&s); err != nil {
			return nil, err
		}
		s.Interface, s.SnapLen, s.SampleEvery, s.SampleOffset, s.InterfaceFilter = "", 0, 0, 0, ""
		s.EBPF = EBPFConfig{}
		s.Capture.EBPF.Direction = ""
	}
	return plain(s), nil
}
