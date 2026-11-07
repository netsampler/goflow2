//go:build !windows || reflow_pcap || reflow_dpdk || reflow_pfring

package pktmon

import (
	"fmt"
	"github.com/netsampler/goflow2/v3/pkg/reflow/config"
	"github.com/netsampler/goflow2/v3/pkg/reflow/source/capture"
)

func New(config.SourceConfig) (*capture.Source, error) {
	return nil, fmt.Errorf("pktmon requires the Windows platform profile")
}
