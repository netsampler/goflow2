//go:build !linux || reflow_pcap || reflow_dpdk || reflow_pfring

package xdp

import (
	"fmt"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func New(config.SourceConfig) (*capture.Source, error) {
	return nil, fmt.Errorf("xdp requires the Linux platform profile")
}
