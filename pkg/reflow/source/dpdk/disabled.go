//go:build !linux || !cgo || !reflow_dpdk || reflow_pcap || reflow_pfring

// Package dpdk provides the dedicated Linux DPDK capture backend.
package dpdk

import (
	"fmt"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func New(config.SourceConfig) (*capture.Source, error) {
	return nil, fmt.Errorf("dpdk requires the Linux dpdk profile, cgo, and the DPDK runtime")
}

func SDKVersion() string { return "unavailable" }

func Cleanup() error { return nil }
