//go:build !linux || !cgo || !reflow_pfring || reflow_pcap || reflow_dpdk

// Package pfring provides the dedicated Linux PF_RING capture profile.
package pfring

import (
	"fmt"

	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func New(config.SourceConfig) (*capture.Source, error) {
	return nil, fmt.Errorf("pfring requires the Linux pfring profile, cgo, and the PF_RING runtime")
}

func SDKVersion() string { return "unavailable" }

func ZCAvailable() bool { return false }
