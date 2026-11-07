//go:build !darwin || reflow_pcap || reflow_dpdk || reflow_pfring

package pktap

import (
	"fmt"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func New(config.SourceConfig) (*capture.Source, error) {
	return nil, fmt.Errorf("pktap requires the macOS platform profile")
}
